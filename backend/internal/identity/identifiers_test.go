package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCanonicalEmailTrimAndLookupForm(t *testing.T) {
	got, err := CanonicalizeIdentifier(IdentifierEmail, "  Alice@Example.COM  ")
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if got != "alice@example.com" {
		t.Fatalf("got %q, want alice@example.com", got)
	}
}

func TestCanonicalEmailDoesNotApplyTurkishLocale(t *testing.T) {
	got, err := CanonicalizeIdentifier(IdentifierEmail, "I@EXAMPLE.COM")
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if got != "i@example.com" {
		t.Fatalf("got %q, want Unicode-default i@example.com", got)
	}
	if strings.ContainsRune(got, '\u0131') {
		t.Fatal("must not apply Turkish locale casing to email identity")
	}
}

func TestCanonicalEmailRejectsBasicInvalidShape(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"no-at-sign",
		"a@b@c.com",
		"@example.com",
		"user@",
		"user@localhost",
		"user @example.com",
		"user@.example.com",
		"user@example.com.",
		"user@exam ple.com",
	}
	for _, raw := range invalid {
		if _, err := CanonicalizeIdentifier(IdentifierEmail, raw); !errors.Is(err, errInvalidIdentifier) {
			t.Fatalf("%q err = %v, want %v", raw, err, errInvalidIdentifier)
		}
	}
}

func TestCanonicalEmailNoGmailOrProviderRewriting(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"John.Doe@gmail.com", "john.doe@gmail.com"},
		{"john.doe+tag@gmail.com", "john.doe+tag@gmail.com"},
		{"j.o.h.n+promo@googlemail.com", "j.o.h.n+promo@googlemail.com"},
		{"User.Name+list@outlook.com", "user.name+list@outlook.com"},
	}
	for _, tc := range cases {
		got, err := CanonicalizeIdentifier(IdentifierEmail, tc.raw)
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("%q → %q, want %q (no provider rewriting)", tc.raw, got, tc.want)
		}
	}
	a, _ := CanonicalizeIdentifier(IdentifierEmail, "johndoe@gmail.com")
	b, _ := CanonicalizeIdentifier(IdentifierEmail, "john.doe@gmail.com")
	if a == b {
		t.Fatal("Gmail-dot folding must not be applied")
	}
}

func TestCanonicalE164ValidAndInvalid(t *testing.T) {
	valid := []string{"+15551234567", "  +905551234567  ", "+12345678", "+123456789012345"}
	for _, raw := range valid {
		got, err := CanonicalizeIdentifier(IdentifierPhone, raw)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if !strings.HasPrefix(got, "+") {
			t.Fatalf("%q canonical %q missing +", raw, got)
		}
	}
	invalid := []string{
		"",
		"905551234567",
		"05551234567",
		"+05551234567",
		"+1234567",
		"+1234567890123456",
		"+90 555 123 4567",
		"+90-555-123-4567",
		"++905551234567",
		"+abc5551234567",
	}
	for _, raw := range invalid {
		if _, err := CanonicalizeIdentifier(IdentifierPhone, raw); !errors.Is(err, errInvalidIdentifier) {
			t.Fatalf("%q err = %v, want %v", raw, err, errInvalidIdentifier)
		}
	}
}

func TestAddIdentifierAndDuplicateConflict(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestIdentifiers(t)
	user := store.putUser(t, User{})

	got, err := svc.Add(ctx, user.ID, IdentifierEmail, "  Owner@Example.com ")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if got.ValueCanonical != "owner@example.com" || got.Kind != IdentifierEmail || got.VerifiedAt != nil || got.RevokedAt != nil {
		t.Fatalf("stored identifier mismatch: %+v", got)
	}

	_, err = svc.Add(ctx, user.ID, IdentifierEmail, "owner@example.com")
	if !errors.Is(err, errIdentifierConflict) {
		t.Fatalf("same-user duplicate err = %v, want %v", err, errIdentifierConflict)
	}

	other := store.putUser(t, User{})
	_, err = svc.Add(ctx, other.ID, IdentifierEmail, "OWNER@example.com")
	if !errors.Is(err, errIdentifierConflict) {
		t.Fatalf("cross-user duplicate err = %v, want %v", err, errIdentifierConflict)
	}
	if errors.Is(errIdentifierConflict, errUnavailable) {
		t.Fatal("duplicate must be an explicit conflict, not unavailability")
	}

	phone, err := svc.Add(ctx, user.ID, IdentifierPhone, "+905551234567")
	if err != nil {
		t.Fatalf("phone add: %v", err)
	}
	list, err := svc.ListActiveForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("active list len = %d, want 2", len(list))
	}
	if phone.ValueCanonical != "+905551234567" {
		t.Fatalf("phone canonical = %q", phone.ValueCanonical)
	}
}

func TestResolveVerifiedIdentifier(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestIdentifiers(t)
	user := store.putUser(t, User{})
	ident, err := svc.Add(ctx, user.ID, IdentifierEmail, "login@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveVerified(ctx, IdentifierEmail, "login@example.com"); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("unverified err = %v, want %v", err, errUnauthenticated)
	}
	if err := svc.MarkVerified(ctx, ident.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ResolveVerified(ctx, IdentifierEmail, "  LOGIN@Example.com ")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("resolved user %v, want %v", got.ID, user.ID)
	}
}

func TestResolveRejectsUnverifiedAndRevoked(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newTestIdentifiers(t)
	user := store.putUser(t, User{})
	ident, err := svc.Add(ctx, user.ID, IdentifierPhone, "+15551234567")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveVerified(ctx, IdentifierPhone, "+15551234567"); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("unverified err = %v, want %v", err, errUnauthenticated)
	}
	if err := svc.MarkVerified(ctx, ident.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Revoke(ctx, ident.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveVerified(ctx, IdentifierPhone, "+15551234567"); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("revoked err = %v, want %v", err, errUnauthenticated)
	}
	if _, err := svc.ResolveVerified(ctx, IdentifierPhone, "+15550000000"); !errors.Is(err, errUnauthenticated) {
		t.Fatalf("unknown err = %v, want %v", err, errUnauthenticated)
	}
	list, err := svc.ListActiveForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("revoked identifier must not list as active: %+v", list)
	}
	_, err = svc.Add(ctx, user.ID, IdentifierPhone, "+15551234567")
	if err != nil {
		t.Fatalf("re-add after revoke: %v", err)
	}
}

func TestResolveRejectsDisabledAndDeletedUser(t *testing.T) {
	ctx := context.Background()
	svc, store, now := newTestIdentifiers(t)
	user := store.putUser(t, User{})
	ident, err := svc.Add(ctx, user.ID, IdentifierEmail, "standing@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkVerified(ctx, ident.ID); err != nil {
		t.Fatal(err)
	}

	disabled := now()
	u := store.users[user.ID]
	u.DisabledAt = &disabled
	store.users[user.ID] = u
	if _, err := svc.ResolveVerified(ctx, IdentifierEmail, "standing@example.com"); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("disabled err = %v, want %v", err, errAccountIneligible)
	}

	u.DisabledAt = nil
	deleted := now()
	u.DeletedAt = &deleted
	store.users[user.ID] = u
	if _, err := svc.ResolveVerified(ctx, IdentifierEmail, "standing@example.com"); !errors.Is(err, errAccountIneligible) {
		t.Fatalf("deleted err = %v, want %v", err, errAccountIneligible)
	}
}

func TestIdentifierStoreFailureIsUnavailable(t *testing.T) {
	ctx := context.Background()
	store := newMemIdentifierStore()
	store.unavailable = true
	svc, err := NewIdentifiers(store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Add(ctx, mustID(t), IdentifierEmail, "a@example.com"); !errors.Is(err, errUnavailable) {
		t.Fatalf("add err = %v, want %v", err, errUnavailable)
	}
	if _, err := svc.ResolveVerified(ctx, IdentifierEmail, "a@example.com"); !errors.Is(err, errUnavailable) {
		t.Fatalf("resolve err = %v, want %v", err, errUnavailable)
	}
	if errors.Is(errUnavailable, errUnauthenticated) || errors.Is(errUnavailable, errIdentifierConflict) {
		t.Fatal("store unavailability must not be an auth or conflict outcome")
	}
}

func TestIdentifierErrorMessagesDoNotIncludeValues(t *testing.T) {
	email := "secret.user+tag@example.com"
	phone := "+905551234567"
	_, err := CanonicalizeIdentifier(IdentifierEmail, "not-an-email")
	if err == nil || strings.Contains(err.Error(), "not-an-email") {
		t.Fatalf("invalid email error leaked input: %v", err)
	}
	_, err = CanonicalizeIdentifier(IdentifierPhone, "05551234567")
	if err == nil || strings.Contains(err.Error(), "0555") {
		t.Fatalf("invalid phone error leaked input: %v", err)
	}
	if strings.Contains(errIdentifierConflict.Error(), email) || strings.Contains(errUnauthenticated.Error(), phone) {
		t.Fatal("sentinel errors must not include identifier values")
	}
}

func newTestIdentifiers(t *testing.T) (*Identifiers, *memIdentifierStore, func() time.Time) {
	t.Helper()
	store := newMemIdentifierStore()
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	svc, err := NewIdentifiers(store, clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, clock
}

type memIdentifierStore struct {
	users       map[ID]User
	idents      map[ID]UserIdentifier
	unavailable bool
}

func newMemIdentifierStore() *memIdentifierStore {
	return &memIdentifierStore{
		users:  make(map[ID]User),
		idents: make(map[ID]UserIdentifier),
	}
}

func (m *memIdentifierStore) putUser(t *testing.T, u User) User {
	t.Helper()
	if u.ID.IsZero() {
		u.ID = mustID(t)
	}
	now := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
		u.UpdatedAt = now
	}
	m.users[u.ID] = u
	return u
}

func (m *memIdentifierStore) check() error {
	if m.unavailable {
		return errUnavailable
	}
	return nil
}

func (m *memIdentifierStore) GetUser(ctx context.Context, id ID) (User, error) {
	if err := m.check(); err != nil {
		return User{}, err
	}
	u, ok := m.users[id]
	if !ok {
		return User{}, errNotFound
	}
	return u, nil
}

func (m *memIdentifierStore) InsertIdentifier(ctx context.Context, identifier UserIdentifier) error {
	if err := m.check(); err != nil {
		return err
	}
	for _, existing := range m.idents {
		if existing.RevokedAt == nil && existing.Kind == identifier.Kind && existing.ValueCanonical == identifier.ValueCanonical {
			return errIdentifierConflict
		}
	}
	m.idents[identifier.ID] = cloneIdentifier(identifier)
	return nil
}

func (m *memIdentifierStore) GetIdentifier(ctx context.Context, id ID) (UserIdentifier, error) {
	if err := m.check(); err != nil {
		return UserIdentifier{}, err
	}
	ident, ok := m.idents[id]
	if !ok {
		return UserIdentifier{}, errNotFound
	}
	return cloneIdentifier(ident), nil
}

func (m *memIdentifierStore) GetActiveIdentifier(ctx context.Context, kind IdentifierKind, valueCanonical string) (UserIdentifier, error) {
	if err := m.check(); err != nil {
		return UserIdentifier{}, err
	}
	for _, ident := range m.idents {
		if ident.Kind == kind && ident.ValueCanonical == valueCanonical && ident.RevokedAt == nil {
			return cloneIdentifier(ident), nil
		}
	}
	return UserIdentifier{}, errNotFound
}

func (m *memIdentifierStore) ListActiveIdentifiersForUser(ctx context.Context, userID ID) ([]UserIdentifier, error) {
	if err := m.check(); err != nil {
		return nil, err
	}
	out := make([]UserIdentifier, 0)
	for _, ident := range m.idents {
		if ident.UserID == userID && ident.RevokedAt == nil {
			out = append(out, cloneIdentifier(ident))
		}
	}
	return out, nil
}

func (m *memIdentifierStore) MarkIdentifierVerified(ctx context.Context, id ID, at time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	ident, ok := m.idents[id]
	if !ok || ident.RevokedAt != nil {
		return errNotFound
	}
	if ident.VerifiedAt == nil {
		ident.VerifiedAt = &at
		m.idents[id] = ident
	}
	return nil
}

func (m *memIdentifierStore) RevokeIdentifier(ctx context.Context, id ID, at time.Time) error {
	if err := m.check(); err != nil {
		return err
	}
	ident, ok := m.idents[id]
	if !ok || ident.RevokedAt != nil {
		return errNotFound
	}
	ident.RevokedAt = &at
	m.idents[id] = ident
	return nil
}

func cloneIdentifier(i UserIdentifier) UserIdentifier {
	if i.VerifiedAt != nil {
		t := *i.VerifiedAt
		i.VerifiedAt = &t
	}
	if i.RevokedAt != nil {
		t := *i.RevokedAt
		i.RevokedAt = &t
	}
	return i
}
