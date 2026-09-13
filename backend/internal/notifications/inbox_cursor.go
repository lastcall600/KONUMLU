package notifications

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const inboxCursorVersion = 1
const defaultInboxLimit = 20
const maxInboxLimit = 50

type inboxCursor struct {
	CreatedAt time.Time
	ID        ID
}

type inboxCursorWire struct {
	V int    `json:"v"`
	T string `json:"t"`
	I string `json:"i"`
}

func encodeInboxCursor(row InboxRow) (string, error) {
	if row.ID.IsZero() || row.CreatedAt.IsZero() {
		return "", errInvalidQuery
	}
	raw, err := json.Marshal(inboxCursorWire{
		V: inboxCursorVersion,
		T: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		I: row.ID.String(),
	})
	if err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeInboxCursor(raw string) (inboxCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return inboxCursor{}, errInvalidQuery
	}
	var wire inboxCursorWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return inboxCursor{}, errInvalidQuery
	}
	if wire.V != inboxCursorVersion || strings.TrimSpace(wire.T) == "" || strings.TrimSpace(wire.I) == "" {
		return inboxCursor{}, errInvalidQuery
	}
	id, err := ParseID(wire.I)
	if err != nil {
		return inboxCursor{}, errInvalidQuery
	}
	ts, err := time.Parse(time.RFC3339Nano, wire.T)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, wire.T)
		if err != nil {
			return inboxCursor{}, errInvalidQuery
		}
	}
	return inboxCursor{CreatedAt: ts.UTC(), ID: id}, nil
}

func normalizeInboxLimit(limit int) (int, error) {
	if limit == 0 {
		return defaultInboxLimit, nil
	}
	if limit < 1 || limit > maxInboxLimit {
		return 0, errInvalidQuery
	}
	return limit, nil
}

func inboxAfter(a, b InboxRow) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}
