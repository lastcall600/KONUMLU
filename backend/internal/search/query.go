package search

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLimit   = 20
	maxLimit       = 50
	maxQueryLen    = 200
	cursorVersion  = 1
	cursorModeRank = "rank"
	cursorModeTime = "time"
)

var (
	priceAmountPattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,8})?$`)
	currencyPattern    = regexp.MustCompile(`^[A-Z]{3}$`)
)

// Query is the public listing discovery input.
type Query struct {
	Q          string
	CategoryID *ID
	MinPrice   *string
	MaxPrice   *string
	Currency   *string
	Viewport   *Viewport
	Cursor     string
	Limit      int
}

type Viewport struct {
	North float64
	South float64
	East  float64
	West  float64
}

type NormalizedQuery struct {
	Q          string
	Tokens     []string
	HasText    bool
	CategoryID *ID
	MinPrice   *big.Rat
	MaxPrice   *big.Rat
	Currency   *string
	Viewport   *Viewport
	Cursor     *opaqueCursor
	Limit      int
}

type opaqueCursor struct {
	Mode        string
	Rank        float64
	PublishedAt time.Time
	ListingID   ID
}

type Page struct {
	Hits       []Hit
	NextCursor string
}

type cursorWire struct {
	V int     `json:"v"`
	M string  `json:"m"`
	R *string `json:"r,omitempty"`
	P *string `json:"p,omitempty"`
	I string  `json:"i"`
}

func (q Query) normalize() (NormalizedQuery, error) {
	out := NormalizedQuery{Limit: q.Limit}
	if out.Limit == 0 {
		out.Limit = defaultLimit
	}
	if out.Limit < 1 || out.Limit > maxLimit {
		return NormalizedQuery{}, errInvalidQuery
	}
	text := strings.TrimSpace(q.Q)
	if len(text) > maxQueryLen {
		return NormalizedQuery{}, errInvalidQuery
	}
	out.Q = text
	if text != "" {
		out.HasText = true
		out.Tokens = tokenize(text)
		if len(out.Tokens) == 0 {
			return NormalizedQuery{}, errInvalidQuery
		}
	}
	if q.CategoryID != nil {
		if q.CategoryID.IsZero() {
			return NormalizedQuery{}, errInvalidQuery
		}
		id := *q.CategoryID
		out.CategoryID = &id
	}
	minP, err := parseOptionalPrice(q.MinPrice)
	if err != nil {
		return NormalizedQuery{}, err
	}
	maxP, err := parseOptionalPrice(q.MaxPrice)
	if err != nil {
		return NormalizedQuery{}, err
	}
	if minP != nil && maxP != nil && minP.Cmp(maxP) > 0 {
		return NormalizedQuery{}, errInvalidQuery
	}
	out.MinPrice = minP
	out.MaxPrice = maxP
	if q.Currency != nil {
		cur := strings.TrimSpace(*q.Currency)
		if !currencyPattern.MatchString(cur) {
			return NormalizedQuery{}, errInvalidQuery
		}
		out.Currency = &cur
	}
	if q.Viewport != nil {
		if err := q.Viewport.validate(); err != nil {
			return NormalizedQuery{}, err
		}
		vp := *q.Viewport
		out.Viewport = &vp
	}
	raw := strings.TrimSpace(q.Cursor)
	if raw != "" {
		cur, err := decodeCursor(raw)
		if err != nil {
			return NormalizedQuery{}, err
		}
		want := cursorModeTime
		if out.HasText {
			want = cursorModeRank
		}
		if cur.Mode != want {
			return NormalizedQuery{}, errInvalidQuery
		}
		out.Cursor = &cur
	}
	return out, nil
}

func (v Viewport) validate() error {
	if v.North < -90 || v.North > 90 || v.South < -90 || v.South > 90 {
		return errInvalidQuery
	}
	if v.East < -180 || v.East > 180 || v.West < -180 || v.West > 180 {
		return errInvalidQuery
	}
	if v.North <= v.South || v.East <= v.West {
		return errInvalidQuery
	}
	return nil
}

func parseOptionalPrice(raw *string) (*big.Rat, error) {
	if raw == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*raw)
	if s == "" || !priceAmountPattern.MatchString(s) {
		return nil, errInvalidQuery
	}
	r := new(big.Rat)
	if _, ok := r.SetString(s); !ok {
		return nil, errInvalidQuery
	}
	return r, nil
}

func tokenize(q string) []string {
	fields := strings.Fields(strings.ToLower(q))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

func encodeCursor(hit Hit, hasText bool) (string, error) {
	wire := cursorWire{V: cursorVersion, I: hit.Doc.ListingID.String()}
	if hasText {
		wire.M = cursorModeRank
		r := strconv.FormatFloat(hit.Rank, 'g', 15, 64)
		wire.R = &r
	} else {
		wire.M = cursorModeTime
		if hit.Doc.PublishedAt == nil {
			return "", errInvalidQuery
		}
		p := hit.Doc.PublishedAt.UTC().Format(time.RFC3339Nano)
		wire.P = &p
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return "", errUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func decodeCursor(raw string) (opaqueCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return opaqueCursor{}, errInvalidQuery
	}
	var wire cursorWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return opaqueCursor{}, errInvalidQuery
	}
	if wire.V != cursorVersion || strings.TrimSpace(wire.I) == "" {
		return opaqueCursor{}, errInvalidQuery
	}
	id, err := ParseID(wire.I)
	if err != nil {
		return opaqueCursor{}, errInvalidQuery
	}
	cur := opaqueCursor{Mode: wire.M, ListingID: id}
	switch wire.M {
	case cursorModeRank:
		if wire.R == nil {
			return opaqueCursor{}, errInvalidQuery
		}
		rank, err := strconv.ParseFloat(*wire.R, 64)
		if err != nil {
			return opaqueCursor{}, errInvalidQuery
		}
		cur.Rank = rank
	case cursorModeTime:
		if wire.P == nil {
			return opaqueCursor{}, errInvalidQuery
		}
		ts, err := time.Parse(time.RFC3339Nano, *wire.P)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, *wire.P)
			if err != nil {
				return opaqueCursor{}, errInvalidQuery
			}
		}
		cur.PublishedAt = ts.UTC()
	default:
		return opaqueCursor{}, errInvalidQuery
	}
	return cur, nil
}

func afterCursor(hit Hit, q NormalizedQuery) bool {
	if q.Cursor == nil {
		return true
	}
	c := q.Cursor
	if q.HasText {
		if hit.Rank < c.Rank {
			return true
		}
		if hit.Rank > c.Rank {
			return false
		}
		return bytes.Compare(hit.Doc.ListingID[:], c.ListingID[:]) < 0
	}
	if hit.Doc.PublishedAt == nil {
		return false
	}
	pub := hit.Doc.PublishedAt.UTC()
	if pub.Before(c.PublishedAt) {
		return true
	}
	if pub.After(c.PublishedAt) {
		return false
	}
	return bytes.Compare(hit.Doc.ListingID[:], c.ListingID[:]) < 0
}

func cmpHits(a, b Hit, hasText bool) bool {
	if hasText {
		if a.Rank != b.Rank {
			return a.Rank > b.Rank
		}
		return bytes.Compare(a.Doc.ListingID[:], b.Doc.ListingID[:]) > 0
	}
	ap, bp := a.Doc.PublishedAt, b.Doc.PublishedAt
	if ap == nil || bp == nil {
		return bytes.Compare(a.Doc.ListingID[:], b.Doc.ListingID[:]) > 0
	}
	if !ap.Equal(*bp) {
		return ap.After(*bp)
	}
	return bytes.Compare(a.Doc.ListingID[:], b.Doc.ListingID[:]) > 0
}

func memoryTextRank(title, description string, tokens []string) (float64, bool) {
	t := strings.ToLower(title)
	d := strings.ToLower(description)
	var rank float64
	for _, tok := range tokens {
		inTitle := strings.Contains(t, tok)
		inDesc := strings.Contains(d, tok)
		if !inTitle && !inDesc {
			return 0, false
		}
		if inTitle {
			rank += 1.0
		}
		if inDesc {
			rank += 0.4
		}
	}
	return rank, true
}

func priceRat(amount *string) *big.Rat {
	if amount == nil {
		return nil
	}
	r := new(big.Rat)
	if _, ok := r.SetString(strings.TrimSpace(*amount)); !ok {
		return nil
	}
	return r
}
