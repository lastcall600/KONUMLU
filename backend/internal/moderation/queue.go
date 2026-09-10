package moderation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const queueCursorVersion = 1

type queueCursor struct {
	CreatedAt time.Time
	ID        ID
	Order     QueueOrder
}

type queueCursorWire struct {
	V int    `json:"v"`
	O string `json:"o"`
	T string `json:"t"`
	I string `json:"i"`
}

func encodeQueueCursor(order QueueOrder, row Report) (string, error) {
	if row.ID.IsZero() || row.CreatedAt.IsZero() {
		return "", errInvalidQuery
	}
	raw, err := json.Marshal(queueCursorWire{
		V: queueCursorVersion,
		O: string(order),
		T: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		I: row.ID.String(),
	})
	if err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeQueueCursor(raw string, order QueueOrder) (queueCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return queueCursor{}, errInvalidQuery
	}
	var wire queueCursorWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return queueCursor{}, errInvalidQuery
	}
	if wire.V != queueCursorVersion || strings.TrimSpace(wire.O) == "" || strings.TrimSpace(wire.T) == "" || strings.TrimSpace(wire.I) == "" {
		return queueCursor{}, errInvalidQuery
	}
	if QueueOrder(wire.O) != order {
		return queueCursor{}, errInvalidQuery
	}
	rid, err := ParseID(wire.I)
	if err != nil {
		return queueCursor{}, errInvalidQuery
	}
	ts, err := time.Parse(time.RFC3339Nano, wire.T)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, wire.T)
		if err != nil {
			return queueCursor{}, errInvalidQuery
		}
	}
	return queueCursor{CreatedAt: ts.UTC(), ID: rid, Order: order}, nil
}

func afterQueueCursor(row Report, cursor *queueCursor, order QueueOrder) bool {
	if cursor == nil {
		return true
	}
	cmp := compareQueue(row, Report{CreatedAt: cursor.CreatedAt, ID: cursor.ID})
	if order == QueueOldest {
		return cmp > 0
	}
	return cmp < 0
}

func compareQueue(a, b Report) int {
	if a.CreatedAt.After(b.CreatedAt) {
		return 1
	}
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}

func normalizeQueueQuery(q QueueQuery) (QueueQuery, error) {
	order, err := ParseQueueOrder(string(q.Order))
	if err != nil {
		return QueueQuery{}, err
	}
	q.Order = order
	if q.Status != nil {
		st, err := ParseStatus(string(*q.Status))
		if err != nil {
			return QueueQuery{}, errInvalidQuery
		}
		q.Status = &st
	}
	if q.TargetType != nil {
		tt, err := ParseTargetType(string(*q.TargetType))
		if err != nil {
			return QueueQuery{}, errInvalidQuery
		}
		q.TargetType = &tt
	}
	if q.ReasonCode != nil {
		rc, err := ParseReasonCode(string(*q.ReasonCode))
		if err != nil {
			return QueueQuery{}, errInvalidQuery
		}
		q.ReasonCode = &rc
	}
	if q.Limit == 0 {
		q.Limit = DefaultQueueLimit
	}
	if q.Limit < 1 || q.Limit > MaxQueueLimit {
		return QueueQuery{}, errInvalidQuery
	}
	return q, nil
}

func matchesQueueFilters(row Report, q QueueQuery) bool {
	if q.Status != nil && row.Status != *q.Status {
		return false
	}
	if q.TargetType != nil && row.TargetType != *q.TargetType {
		return false
	}
	if q.ReasonCode != nil && row.ReasonCode != *q.ReasonCode {
		return false
	}
	return true
}
