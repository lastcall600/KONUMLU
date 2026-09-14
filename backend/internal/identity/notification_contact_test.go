package identity

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"backend/internal/identity/contracts"
)

func TestNotificationContactDoesNotPrintDestination(t *testing.T) {
	c := contracts.NotificationContact{Kind: contracts.NotificationContactEmail, Value: "owner@example.test"}
	if strings.Contains(c.String(), "owner@") || strings.Contains(fmt.Sprintf("%#v", c), "owner@") {
		t.Fatal("contact leaked destination")
	}
	if strings.Contains(fmt.Sprintf("%v", c), "owner@") {
		t.Fatal("fmt leaked destination")
	}
}

func TestResolveVerifiedContactReturnsCanonicalInMemoryOnly(t *testing.T) {
	store := newMemIdentifierStore()
	user := store.putUser(t, User{})
	svc, err := NewIdentifiers(store, func() time.Time {
		return time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	ident, err := svc.Add(context.Background(), user.ID, IdentifierEmail, "Owner@Example.COM")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkIdentifierVerified(context.Background(), ident.ID, time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	r, err := NewNotificationEligibilityReader(store)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.ResolveVerifiedContact(context.Background(), contracts.ID(user.ID), contracts.NotificationContactEmail)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != "owner@example.com" {
		t.Fatalf("canonical = %q", got.Value)
	}
	elig, err := r.ReadNotificationEligibility(context.Background(), contracts.ID(user.ID))
	if err != nil || !elig.EmailVerified || elig.Deleted {
		t.Fatalf("elig=%+v err=%v", elig, err)
	}
}
