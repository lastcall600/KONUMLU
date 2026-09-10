package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"backend/internal/messaging"
)

const (
	sessionCookieName = "__Host-konumlu_session"
	csrfCookieName    = "__Host-konumlu_csrf"
	csrfHeaderName    = "X-CSRF-Token"
	maxJSONBytes      = 16 << 10
)

var ErrUnauthenticated = errors.New("unauthenticated")

type sessionResolver interface {
	Resolve(ctx context.Context, rawToken string) (messaging.ID, error)
}

type Handler struct {
	sessions sessionResolver
	svc      *messaging.Service
	origins  map[string]struct{}
}

func New(sessions sessionResolver, svc *messaging.Service, allowedOrigins []string) (*Handler, error) {
	if sessions == nil || svc == nil {
		return nil, messaging.ErrUnavailable
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" || origin == "*" || strings.Contains(origin, "*") {
			return nil, messaging.ErrUnavailable
		}
		origins[origin] = struct{}{}
	}
	if len(origins) == 0 {
		return nil, messaging.ErrUnavailable
	}
	return &Handler{sessions: sessions, svc: svc, origins: origins}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/messaging/conversations", h.create)
	mux.HandleFunc("GET /v1/messaging/conversations", h.list)
	mux.HandleFunc("GET /v1/messaging/conversations/{conversationId}", h.get)
	mux.HandleFunc("GET /v1/messaging/conversations/{conversationId}/messages", h.listMessages)
	mux.HandleFunc("POST /v1/messaging/conversations/{conversationId}/messages", h.send)
	mux.HandleFunc("POST /v1/messaging/conversations/{conversationId}/read", h.markRead)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	in, ok := parseCreate(w, r)
	if !ok {
		return
	}
	conv, err := h.svc.CreateConversation(r.Context(), userID, in)
	if err != nil {
		writeMessagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toConversationDTO(conv, userID))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	rows, err := h.svc.ListConversations(r.Context(), userID)
	if err != nil {
		writeMessagingError(w, err)
		return
	}
	out := make([]conversationSummaryDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSummaryDTO(row, userID))
	}
	writeJSON(w, http.StatusOK, conversationListDTO{Conversations: out})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	conversationID, ok := parseConversationID(w, r)
	if !ok {
		return
	}
	conv, err := h.svc.GetConversation(r.Context(), userID, conversationID)
	if err != nil {
		writeMessagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toConversationDTO(conv, userID))
}

func (h *Handler) listMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	conversationID, ok := parseConversationID(w, r)
	if !ok {
		return
	}
	msgs, err := h.svc.ListMessages(r.Context(), userID, conversationID)
	if err != nil {
		writeMessagingError(w, err)
		return
	}
	out := make([]messageDTO, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, toMessageDTO(msg))
	}
	writeJSON(w, http.StatusOK, messageListDTO{Messages: out})
}

func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	conversationID, ok := parseConversationID(w, r)
	if !ok {
		return
	}
	body, ok := parseSend(w, r)
	if !ok {
		return
	}
	msg, err := h.svc.SendMessage(r.Context(), userID, conversationID, body)
	if err != nil {
		writeMessagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMessageDTO(msg))
}

func (h *Handler) markRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireOriginSessionCSRF(w, r)
	if !ok {
		return
	}
	conversationID, ok := parseConversationID(w, r)
	if !ok {
		return
	}
	if err := h.svc.MarkRead(r.Context(), userID, conversationID); err != nil {
		writeMessagingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, readDTO{Read: true})
}

func parseCreate(w http.ResponseWriter, r *http.Request) (messaging.ID, bool) {
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return messaging.ID{}, false
	}
	if len(req.UserID) > 0 || len(req.BuyerUserID) > 0 || len(req.SellerUserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return messaging.ID{}, false
	}
	raw := strings.TrimSpace(req.ListingID)
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return messaging.ID{}, false
	}
	id, err := messaging.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return messaging.ID{}, false
	}
	return id, true
}

func parseSend(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req sendRequest
	if !decodeJSON(w, r, &req) {
		return "", false
	}
	if len(req.SenderUserID) > 0 || len(req.UserID) > 0 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return req.Body, true
}

func parseConversationID(w http.ResponseWriter, r *http.Request) (messaging.ID, bool) {
	raw := strings.TrimSpace(r.PathValue("conversationId"))
	if raw == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, "/\\") {
		writeError(w, http.StatusBadRequest, "bad_request")
		return messaging.ID{}, false
	}
	id, err := messaging.ParseID(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return messaging.ID{}, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	limited := http.MaxBytesReader(w, r.Body, maxJSONBytes)
	dec := json.NewDecoder(limited)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

func (h *Handler) requireOriginSessionCSRF(w http.ResponseWriter, r *http.Request) (messaging.ID, bool) {
	if !h.requireOrigin(w, r) {
		return messaging.ID{}, false
	}
	userID, ok := h.requireSession(w, r)
	if !ok {
		return messaging.ID{}, false
	}
	if !csrfOK(r) {
		writeError(w, http.StatusForbidden, "forbidden")
		return messaging.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireSession(w http.ResponseWriter, r *http.Request) (messaging.ID, bool) {
	raw, ok := readCookie(r, sessionCookieName)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return messaging.ID{}, false
	}
	userID, err := h.sessions.Resolve(r.Context(), raw)
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			writeError(w, http.StatusUnauthorized, "unauthenticated")
			return messaging.ID{}, false
		}
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return messaging.ID{}, false
	}
	if userID.IsZero() {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return messaging.ID{}, false
	}
	return userID, true
}

func (h *Handler) requireOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || origin == "null" {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	if _, ok := h.origins[origin]; !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type createRequest struct {
	ListingID    string          `json:"listingId"`
	UserID       json.RawMessage `json:"userId"`
	BuyerUserID  json.RawMessage `json:"buyerUserId"`
	SellerUserID json.RawMessage `json:"sellerUserId"`
}

type sendRequest struct {
	Body         string          `json:"body"`
	SenderUserID json.RawMessage `json:"senderUserId"`
	UserID       json.RawMessage `json:"userId"`
}

type conversationDTO struct {
	ConversationID    string `json:"conversationId"`
	ListingID         string `json:"listingId"`
	CounterpartUserID string `json:"counterpartUserId"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type conversationSummaryDTO struct {
	ConversationID     string  `json:"conversationId"`
	ListingID          string  `json:"listingId"`
	CounterpartUserID  string  `json:"counterpartUserId"`
	LastMessagePreview string  `json:"lastMessagePreview"`
	LastMessageAt      *string `json:"lastMessageAt"`
	UnreadCount        int     `json:"unreadCount"`
	UpdatedAt          string  `json:"updatedAt"`
}

type conversationListDTO struct {
	Conversations []conversationSummaryDTO `json:"conversations"`
}

type messageDTO struct {
	MessageID      string `json:"messageId"`
	ConversationID string `json:"conversationId"`
	SenderUserID   string `json:"senderUserId"`
	Body           string `json:"body"`
	CreatedAt      string `json:"createdAt"`
}

type messageListDTO struct {
	Messages []messageDTO `json:"messages"`
}

type readDTO struct {
	Read bool `json:"read"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toConversationDTO(conv messaging.Conversation, userID messaging.ID) conversationDTO {
	return conversationDTO{
		ConversationID:    conv.ID.String(),
		ListingID:         conv.ListingID.String(),
		CounterpartUserID: conv.Counterpart(userID).String(),
		CreatedAt:         conv.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         conv.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toSummaryDTO(row messaging.ConversationSummary, userID messaging.ID) conversationSummaryDTO {
	dto := conversationSummaryDTO{
		ConversationID:     row.ID.String(),
		ListingID:          row.ListingID.String(),
		CounterpartUserID:  row.Counterpart(userID).String(),
		LastMessagePreview: row.LastMessagePreview,
		UnreadCount:        row.UnreadCount,
		UpdatedAt:          row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if row.LastMessageAt != nil {
		at := row.LastMessageAt.UTC().Format(time.RFC3339)
		dto.LastMessageAt = &at
	}
	return dto
}

func toMessageDTO(msg messaging.Message) messageDTO {
	return messageDTO{
		MessageID:      msg.ID.String(),
		ConversationID: msg.ConversationID.String(),
		SenderUserID:   msg.SenderUserID.String(),
		Body:           msg.Body,
		CreatedAt:      msg.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func writeMessagingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, messaging.ErrZeroID), errors.Is(err, messaging.ErrInvalidBody),
		errors.Is(err, messaging.ErrSelfConversation):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, messaging.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, messaging.ErrUnavailable), errors.Is(err, messaging.ErrStoreRequired),
		errors.Is(err, messaging.ErrListingsReq):
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readCookie(r *http.Request, name string) (string, bool) {
	c, err := r.Cookie(name)
	if err != nil || c == nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func csrfOK(r *http.Request) bool {
	cookie, ok := readCookie(r, csrfCookieName)
	if !ok {
		return false
	}
	header := strings.TrimSpace(r.Header.Get(csrfHeaderName))
	if header == "" || len(header) != len(cookie) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(header), []byte(cookie)) == 1
}
