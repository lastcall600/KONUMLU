package moderation

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const caseCursorVersion = 1

type caseCursor struct {
	CreatedAt time.Time
	ID        ID
}

type caseCursorWire struct {
	V int    `json:"v"`
	T string `json:"t"`
	I string `json:"i"`
}

func encodeCaseCursor(row Case) (string, error) {
	if row.ID.IsZero() || row.CreatedAt.IsZero() {
		return "", errInvalidQuery
	}
	raw, err := json.Marshal(caseCursorWire{
		V: caseCursorVersion,
		T: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		I: row.ID.String(),
	})
	if err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCaseCursor(raw string) (caseCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return caseCursor{}, errInvalidQuery
	}
	var wire caseCursorWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return caseCursor{}, errInvalidQuery
	}
	if wire.V != caseCursorVersion || strings.TrimSpace(wire.T) == "" || strings.TrimSpace(wire.I) == "" {
		return caseCursor{}, errInvalidQuery
	}
	id, err := ParseID(wire.I)
	if err != nil {
		return caseCursor{}, errInvalidQuery
	}
	ts, err := time.Parse(time.RFC3339Nano, wire.T)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, wire.T)
		if err != nil {
			return caseCursor{}, errInvalidQuery
		}
	}
	return caseCursor{CreatedAt: ts.UTC(), ID: id}, nil
}

func afterCaseCursor(row Case, cursor *caseCursor) bool {
	if cursor == nil {
		return true
	}
	return compareCase(row, Case{CreatedAt: cursor.CreatedAt, ID: cursor.ID}) < 0
}

func compareCase(a, b Case) int {
	if a.CreatedAt.After(b.CreatedAt) {
		return 1
	}
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}

func normalizeCaseQuery(q CaseQuery) (CaseQuery, error) {
	if q.Status != nil {
		st, err := ParseCaseStatus(string(*q.Status))
		if err != nil {
			return CaseQuery{}, errInvalidQuery
		}
		q.Status = &st
	}
	if q.Priority != nil {
		pr, err := ParseCasePriority(string(*q.Priority))
		if err != nil {
			return CaseQuery{}, errInvalidQuery
		}
		q.Priority = &pr
	}
	if q.SubjectType != nil {
		tt, err := ParseTargetType(string(*q.SubjectType))
		if err != nil {
			return CaseQuery{}, errInvalidQuery
		}
		q.SubjectType = &tt
	}
	if q.Limit == 0 {
		q.Limit = DefaultCaseLimit
	}
	if q.Limit < 1 || q.Limit > MaxCaseLimit {
		return CaseQuery{}, errInvalidQuery
	}
	return q, nil
}

func matchesCaseFilters(row Case, q CaseQuery) bool {
	if q.Status != nil && row.Status != *q.Status {
		return false
	}
	if q.Priority != nil && row.Priority != *q.Priority {
		return false
	}
	if q.SubjectType != nil && row.SubjectType != *q.SubjectType {
		return false
	}
	return true
}
