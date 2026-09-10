package reviews

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

const cursorVersion = 1

type listingCursor struct {
	CreatedAt time.Time
	ID        ID
}

type cursorWire struct {
	V int    `json:"v"`
	L string `json:"l"`
	T string `json:"t"`
	I string `json:"i"`
}

func encodePublicCursor(listingID ID, row PublicReview) (string, error) {
	if listingID.IsZero() || row.ID.IsZero() || row.CreatedAt.IsZero() {
		return "", errInvalidQuery
	}
	raw, err := json.Marshal(cursorWire{
		V: cursorVersion,
		L: listingID.String(),
		T: row.CreatedAt.UTC().Format(time.RFC3339Nano),
		I: row.ID.String(),
	})
	if err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodePublicCursor(raw string, listingID ID) (listingCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return listingCursor{}, errInvalidQuery
	}
	var wire cursorWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return listingCursor{}, errInvalidQuery
	}
	if wire.V != cursorVersion || strings.TrimSpace(wire.L) == "" || strings.TrimSpace(wire.T) == "" || strings.TrimSpace(wire.I) == "" {
		return listingCursor{}, errInvalidQuery
	}
	lid, err := ParseID(wire.L)
	if err != nil || lid != listingID {
		return listingCursor{}, errInvalidQuery
	}
	rid, err := ParseID(wire.I)
	if err != nil {
		return listingCursor{}, errInvalidQuery
	}
	ts, err := time.Parse(time.RFC3339Nano, wire.T)
	if err != nil {
		ts, err = time.Parse(time.RFC3339, wire.T)
		if err != nil {
			return listingCursor{}, errInvalidQuery
		}
	}
	return listingCursor{CreatedAt: ts.UTC(), ID: rid}, nil
}

func afterListingCursor(row Review, cursor *listingCursor) bool {
	if cursor == nil {
		return true
	}
	created := row.CreatedAt.UTC()
	if created.Before(cursor.CreatedAt) {
		return true
	}
	if created.After(cursor.CreatedAt) {
		return false
	}
	return bytes.Compare(row.ID[:], cursor.ID[:]) < 0
}

func compareListingReviews(a, b Review) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return bytes.Compare(a.ID[:], b.ID[:]) > 0
}

func normalizePublicLimit(limit int) (int, error) {
	if limit == 0 {
		limit = MaxPublicReviews
	}
	if limit < 1 || limit > MaxPublicReviews {
		return 0, errInvalidQuery
	}
	return limit, nil
}
