package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"backend/internal/eids"
	"backend/internal/eids/trdecision"
	"backend/internal/platform/observability"
)

const (
	maxDecisionBytes = 16 << 10
	routePath        = "/internal/tr-compliance/v1/verification-decisions"
)

type Handler struct {
	svc   *eids.Service
	token string
	now   func() time.Time
}

func New(svc *eids.Service, token string, now func() time.Time) (*Handler, error) {
	if svc == nil || strings.TrimSpace(token) == "" {
		return nil, eids.ErrUnavailable
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Handler{svc: svc, token: token, now: now}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	if h == nil || mux == nil {
		return
	}
	mux.Handle("POST "+routePath, h)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := h.now()
	log := observability.FromContext(r.Context())
	if !h.authorize(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		logSafe(log, r, "", "", "", "", "auth_failed", http.StatusUnauthorized, start)
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxDecisionBytes)
	env, err := trdecision.DecodeEnvelope(body)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, trdecision.ErrForbiddenField) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": "bad_request"})
		logSafe(log, r, env.Claims.DecisionID, string(env.Claims.VerificationType), string(env.Claims.Status), env.KeyID, "malformed", status, start)
		return
	}
	got, err := h.svc.IngestSignedDecision(r.Context(), env)
	if err != nil {
		status, code, class := mapIngestErr(err)
		writeJSON(w, status, map[string]string{"error": code})
		logSafe(log, r, env.Claims.DecisionID, string(env.Claims.VerificationType), string(env.Claims.Status), env.KeyID, class, status, start)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"decision_id": got.DecisionID,
		"status":      string(got.Status),
		"idempotent":  got.Idempotent,
	})
	class := "applied"
	if got.Idempotent {
		class = "idempotent"
	}
	logSafe(log, r, got.DecisionID, string(got.Verification.VerificationType), string(got.Status), env.KeyID, class, http.StatusOK, start)
}

func (h *Handler) authorize(r *http.Request) bool {
	if h == nil || h.token == "" {
		return false
	}
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	const prefix = "Bearer "
	if !strings.HasPrefix(raw, prefix) {
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	return tokenEqual(h.token, got)
}

func tokenEqual(want, got string) bool {
	sumWant := sha256.Sum256([]byte(want))
	sumGot := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(sumWant[:], sumGot[:]) == 1
}

func mapIngestErr(err error) (int, string, string) {
	switch {
	case errors.Is(err, trdecision.ErrInvalidSignature), errors.Is(err, trdecision.ErrUnknownKey),
		errors.Is(err, trdecision.ErrInvalidAudience), errors.Is(err, eids.ErrDecisionRejected):
		return http.StatusForbidden, "forbidden", "verify_failed"
	case errors.Is(err, trdecision.ErrExpired):
		return http.StatusGone, "expired", "expired"
	case errors.Is(err, trdecision.ErrStale), errors.Is(err, trdecision.ErrFutureIssuedAt):
		return http.StatusGone, "stale", "stale"
	case errors.Is(err, eids.ErrReplayConflict):
		return http.StatusConflict, "conflict", "replay_conflict"
	case errors.Is(err, eids.ErrUnmappedSubject), errors.Is(err, eids.ErrNotFound):
		return http.StatusForbidden, "forbidden", "unmapped_subject"
	case errors.Is(err, eids.ErrInvalidVerification), errors.Is(err, trdecision.ErrUnsupportedSchema),
		errors.Is(err, trdecision.ErrUnsupportedType), errors.Is(err, trdecision.ErrUnsupportedStatus),
		errors.Is(err, trdecision.ErrMalformedEnvelope), errors.Is(err, trdecision.ErrForbiddenField):
		return http.StatusBadRequest, "bad_request", "malformed"
	default:
		return http.StatusServiceUnavailable, "unavailable", "unavailable"
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func logSafe(log *slog.Logger, r *http.Request, decisionID, vtype, status, keyID, class string, httpStatus int, start time.Time) {
	if log == nil {
		return
	}
	reqID := r.Header.Get("X-Request-Id")
	if reqID == "" {
		reqID = observability.TraceID(r.Context())
	}
	log.Info("tr_compliance_decision",
		"request_id", reqID,
		"decision_id", decisionID,
		"verification_type", vtype,
		"decision_status", status,
		"key_id", keyID,
		"result_class", class,
		"http_status", httpStatus,
		"latency_ms", time.Since(start).Milliseconds(),
		"path", r.URL.Path,
	)
}
