package identity

import (
	"bytes"
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseID(id.String())
	if err != nil {
		t.Fatal(err)
	}
	if got != id {
		t.Fatalf("got %s want %s", got, id)
	}
	if _, err := ParseID("not-a-uuid"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestNewIDIsUUIDV4(t *testing.T) {
	id, err := NewID()
	if err != nil {
		t.Fatalf("NewID: %v", err)
	}
	if id.IsZero() {
		t.Fatal("id is zero")
	}
	if version := id[6] >> 4; version != 4 {
		t.Fatalf("uuid version = %d, want 4", version)
	}
	if variant := id[8] >> 6; variant != 2 {
		t.Fatalf("uuid variant = %d, want RFC 4122", variant)
	}
}

func TestHashSessionSecret(t *testing.T) {
	raw := bytes.Repeat([]byte{0xab}, MinSessionSecretBytes)
	hash, err := HashSessionSecret(raw)
	if err != nil {
		t.Fatalf("HashSessionSecret: %v", err)
	}
	if len(hash) != TokenHashSize {
		t.Fatalf("hash len = %d, want %d", len(hash), TokenHashSize)
	}
	if bytes.Equal(hash, raw) {
		t.Fatal("stored hash must not equal raw secret")
	}
	_, err = HashSessionSecret(raw[:MinSessionSecretBytes-1])
	if err != errWeakSessionSecret {
		t.Fatalf("short secret err = %v, want %v", err, errWeakSessionSecret)
	}
}

func TestUserEligibleForSession(t *testing.T) {
	now := time.Now()
	u := User{ID: mustID(t), CreatedAt: now, UpdatedAt: now}
	if !u.EligibleForSession() {
		t.Fatal("active user should be eligible")
	}
	u.DisabledAt = &now
	if u.EligibleForSession() {
		t.Fatal("disabled user must not be eligible")
	}
	u.DisabledAt = nil
	u.DeletedAt = &now
	if u.EligibleForSession() {
		t.Fatal("deleted user must not be eligible")
	}
}

func TestSessionDurableValid(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	s := validSession(t, now)
	if err := s.DurableValid(now); err != nil {
		t.Fatalf("valid session: %v", err)
	}
	revoked := now
	s.RevokedAt = &revoked
	if err := s.DurableValid(now); err != errSessionRevoked {
		t.Fatalf("revoked err = %v, want %v", err, errSessionRevoked)
	}
	s.RevokedAt = nil
	s.IdleExpiresAt = now
	if err := s.DurableValid(now); err != errSessionIdleExpired {
		t.Fatalf("idle err = %v, want %v", err, errSessionIdleExpired)
	}
	s.IdleExpiresAt = now.Add(time.Hour)
	s.AbsoluteExpiresAt = now
	if err := s.DurableValid(now); err != errSessionAbsExpired {
		t.Fatalf("absolute err = %v, want %v", err, errSessionAbsExpired)
	}
}

func TestAuthorizeSessionFailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	user := User{ID: mustID(t), CreatedAt: now, UpdatedAt: now}
	device := Device{ID: mustID(t), UserID: user.ID, CreatedAt: now, LastSeenAt: now}
	session := validSession(t, now)
	session.UserID = user.ID
	session.DeviceID = &device.ID

	if err := AuthorizeSession(now, user, &device, session); err != nil {
		t.Fatalf("authorize: %v", err)
	}

	disabled := now
	user.DisabledAt = &disabled
	if err := AuthorizeSession(now, user, &device, session); err != errAccountIneligible {
		t.Fatalf("disabled err = %v, want %v", err, errAccountIneligible)
	}
	user.DisabledAt = nil
	revoked := now
	device.RevokedAt = &revoked
	if err := AuthorizeSession(now, user, &device, session); err != errDeviceRevoked {
		t.Fatalf("device err = %v, want %v", err, errDeviceRevoked)
	}
}

func mustID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func validSession(t *testing.T, now time.Time) Session {
	t.Helper()
	hash, err := HashSessionSecret(bytes.Repeat([]byte{1}, MinSessionSecretBytes))
	if err != nil {
		t.Fatal(err)
	}
	return Session{
		ID:                mustID(t),
		UserID:            mustID(t),
		TokenHash:         hash,
		CreatedAt:         now,
		LastSeenAt:        now,
		IdleExpiresAt:     now.Add(time.Hour),
		AbsoluteExpiresAt: now.Add(24 * time.Hour),
	}
}
