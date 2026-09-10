package messaging

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxMessageBytes  = 4000
	MaxConversations = 50
	MaxMessages      = 100
	MaxPreviewRunes  = 160
)

var (
	errZeroID           = errors.New("messaging id must not be zero")
	errStoreRequired    = errors.New("messaging store required")
	errUnavailable      = errors.New("messaging unavailable")
	errNotFound         = errors.New("conversation not found")
	errListingsReq      = errors.New("listings source required")
	errInvalidBody      = errors.New("invalid message body")
	errSelfConversation = errors.New("self conversation")
	errConflict         = errors.New("conversation conflict")
)

var (
	ErrZeroID           = errZeroID
	ErrStoreRequired    = errStoreRequired
	ErrUnavailable      = errUnavailable
	ErrNotFound         = errNotFound
	ErrListingsReq      = errListingsReq
	ErrInvalidBody      = errInvalidBody
	ErrSelfConversation = errSelfConversation
)

// ID is a conversation, message, user, or listing UUID. Messaging does not own those tables.
type ID [16]byte

func NewID() (ID, error) {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		return ID{}, err
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return id, nil
}

func ParseID(s string) (ID, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 32 {
		return ID{}, errZeroID
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != 16 {
		return ID{}, errZeroID
	}
	var id ID
	copy(id[:], b)
	if id.IsZero() {
		return ID{}, errZeroID
	}
	return id, nil
}

func (id ID) IsZero() bool {
	return id == ID{}
}

func (id ID) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:])
}

// Conversation is a listing-scoped buyer/seller thread.
type Conversation struct {
	ID           ID
	ListingID    ID
	BuyerUserID  ID
	SellerUserID ID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (c Conversation) Validate() error {
	if c.ID.IsZero() || c.ListingID.IsZero() || c.BuyerUserID.IsZero() || c.SellerUserID.IsZero() {
		return errZeroID
	}
	if c.BuyerUserID == c.SellerUserID {
		return errSelfConversation
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() {
		return errUnavailable
	}
	return nil
}

func (c Conversation) HasParticipant(userID ID) bool {
	if userID.IsZero() {
		return false
	}
	return c.BuyerUserID == userID || c.SellerUserID == userID
}

func (c Conversation) Counterpart(userID ID) ID {
	if userID == c.BuyerUserID {
		return c.SellerUserID
	}
	if userID == c.SellerUserID {
		return c.BuyerUserID
	}
	return ID{}
}

// Message is plain text. It is stored as submitted after trim; it is not HTML or Markdown.
type Message struct {
	ID             ID
	ConversationID ID
	SenderUserID   ID
	Body           string
	CreatedAt      time.Time
}

func (m Message) Validate() error {
	if m.ID.IsZero() || m.ConversationID.IsZero() || m.SenderUserID.IsZero() {
		return errZeroID
	}
	if _, err := NormalizeMessageBody(m.Body); err != nil {
		return err
	}
	if m.CreatedAt.IsZero() {
		return errUnavailable
	}
	return nil
}

type ConversationSummary struct {
	Conversation
	CounterpartUserID  ID
	LastMessagePreview string
	LastMessageAt      *time.Time
	UnreadCount        int
}

func NormalizeMessageBody(raw string) (string, error) {
	body := strings.TrimSpace(raw)
	if body == "" {
		return "", errInvalidBody
	}
	if len(body) > MaxMessageBytes {
		return "", errInvalidBody
	}
	return body, nil
}

func PreviewBody(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if utf8.RuneCountInString(body) <= MaxPreviewRunes {
		return body
	}
	runes := []rune(body)
	return string(runes[:MaxPreviewRunes])
}

func afterRead(msg Message, last Message) bool {
	if msg.CreatedAt.After(last.CreatedAt) {
		return true
	}
	if msg.CreatedAt.Equal(last.CreatedAt) && msg.ID.String() > last.ID.String() {
		return true
	}
	return false
}
