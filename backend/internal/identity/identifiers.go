package identity

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	e164MinDigits = 8
	e164MaxDigits = 15
)

// Identifiers is the email/phone login-identifier lifecycle (no HTTP, OTP, or notifications).
type Identifiers struct {
	store identifierStore
	now   func() time.Time
}

func NewIdentifiers(store identifierStore, now func() time.Time) (*Identifiers, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Identifiers{store: store, now: now}, nil
}

func (s *Identifiers) Add(ctx context.Context, userID ID, kind IdentifierKind, raw string) (UserIdentifier, error) {
	canonical, err := CanonicalizeIdentifier(kind, raw)
	if err != nil {
		return UserIdentifier{}, err
	}
	if userID.IsZero() {
		return UserIdentifier{}, errZeroID
	}
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		return UserIdentifier{}, mapLookupErr(err, errAccountIneligible)
	}
	if !user.EligibleForSession() || user.ID != userID {
		return UserIdentifier{}, errAccountIneligible
	}
	id, err := NewID()
	if err != nil {
		return UserIdentifier{}, errUnavailable
	}
	ident := UserIdentifier{
		ID:             id,
		UserID:         userID,
		Kind:           kind,
		ValueCanonical: canonical,
		CreatedAt:      s.now(),
	}
	if err := ident.Validate(); err != nil {
		return UserIdentifier{}, err
	}
	if err := s.store.InsertIdentifier(ctx, ident); err != nil {
		return UserIdentifier{}, mapIdentifierStoreErr(err)
	}
	return ident, nil
}

func (s *Identifiers) MarkVerified(ctx context.Context, id ID) error {
	if id.IsZero() {
		return errZeroID
	}
	ident, err := s.store.GetIdentifier(ctx, id)
	if err != nil {
		return mapIdentifierStoreErr(err)
	}
	if !ident.Active() {
		return errNotFound
	}
	return mapIdentifierStoreErr(s.store.MarkIdentifierVerified(ctx, id, s.now()))
}

func (s *Identifiers) Revoke(ctx context.Context, id ID) error {
	if id.IsZero() {
		return errZeroID
	}
	return mapIdentifierStoreErr(s.store.RevokeIdentifier(ctx, id, s.now()))
}

func (s *Identifiers) ListActiveForUser(ctx context.Context, userID ID) ([]UserIdentifier, error) {
	if userID.IsZero() {
		return nil, errZeroID
	}
	list, err := s.store.ListActiveIdentifiersForUser(ctx, userID)
	if err != nil {
		return nil, mapIdentifierStoreErr(err)
	}
	out := make([]UserIdentifier, 0, len(list))
	for _, ident := range list {
		if ident.Active() {
			out = append(out, ident)
		}
	}
	return out, nil
}

// ResolveVerified returns the eligible user bound to a verified, active identifier.
// Unknown, unverified, and revoked identifiers map to unauthenticated.
func (s *Identifiers) ResolveVerified(ctx context.Context, kind IdentifierKind, raw string) (User, error) {
	canonical, err := CanonicalizeIdentifier(kind, raw)
	if err != nil {
		return User{}, errUnauthenticated
	}
	ident, err := s.store.GetActiveIdentifier(ctx, kind, canonical)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return User{}, errUnauthenticated
		}
		return User{}, mapIdentifierStoreErr(err)
	}
	if !ident.VerifiedActive() {
		return User{}, errUnauthenticated
	}
	user, err := s.store.GetUser(ctx, ident.UserID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return User{}, errUnauthenticated
		}
		return User{}, mapIdentifierStoreErr(err)
	}
	if !user.EligibleForSession() || user.ID != ident.UserID {
		return User{}, errAccountIneligible
	}
	return user, nil
}

// CanonicalizeIdentifier trims, validates, and returns the lookup form.
// Email uses Unicode default lowercasing (not a UI/Turkish locale). Phone accepts E.164 only.
func CanonicalizeIdentifier(kind IdentifierKind, raw string) (string, error) {
	switch kind {
	case IdentifierEmail:
		return canonicalizeEmail(raw)
	case IdentifierPhone:
		return canonicalizeE164(raw)
	default:
		return "", errInvalidIdentifier
	}
}

func canonicalizeEmail(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || !utf8.ValidString(s) {
		return "", errInvalidIdentifier
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return "", errInvalidIdentifier
	}
	if containsWhitespace(local) || containsWhitespace(domain) {
		return "", errInvalidIdentifier
	}
	if !basicDomainShape(domain) || !basicLocalShape(local) {
		return "", errInvalidIdentifier
	}
	return strings.ToLower(local) + "@" + strings.ToLower(domain), nil
}

func canonicalizeE164(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if !strings.HasPrefix(s, "+") {
		return "", errInvalidIdentifier
	}
	digits := s[1:]
	n := len(digits)
	if n < e164MinDigits || n > e164MaxDigits {
		return "", errInvalidIdentifier
	}
	if digits[0] < '1' || digits[0] > '9' {
		return "", errInvalidIdentifier
	}
	for i := 1; i < n; i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", errInvalidIdentifier
		}
	}
	return s, nil
}

func basicLocalShape(local string) bool {
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return false
	}
	return true
}

func basicDomainShape(domain string) bool {
	if !strings.Contains(domain, ".") {
		return false
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return false
	}
	return true
}

func containsWhitespace(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func mapIdentifierStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errUnavailable) || errors.Is(err, errZeroID) ||
		errors.Is(err, errInvalidIdentifier) || errors.Is(err, errIdentifierConflict) ||
		errors.Is(err, errAccountIneligible) || errors.Is(err, errUnauthenticated) {
		return err
	}
	return errUnavailable
}
