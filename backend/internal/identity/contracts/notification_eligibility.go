package contracts

import (
	"context"
	"fmt"
)

var (
	ErrNotificationEligibilityUnavailable = ErrUnavailable
	ErrNotificationUserNotFound           = ErrNotFound
	ErrNotificationContactNotFound        = ErrNotFound
)

// NotificationEligibility is Identity-derived destination/account state.
// It never includes email, phone, or other contact values.
type NotificationEligibility struct {
	EmailVerified bool
	PhoneVerified bool
	Deleted       bool
	Disabled      bool
}

// NotificationEligibilityReader is the only Identity surface Notifications may
// use to decide email/SMS destination eligibility.
type NotificationEligibilityReader interface {
	ReadNotificationEligibility(ctx context.Context, userID ID) (NotificationEligibility, error)
}

const (
	NotificationContactEmail = "email"
	NotificationContactPhone = "phone"
)

// NotificationContact is in-memory only. It must not be logged, stored in
// notification tables, or returned on HTTP.
type NotificationContact struct {
	Kind  string
	Value string
}

func (NotificationContact) String() string {
	return "identity.NotificationContact"
}

func (NotificationContact) GoString() string {
	return "identity.NotificationContact{}"
}

func (c NotificationContact) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte("identity.NotificationContact"))
}

// NotificationContactResolver returns a verified destination to the dispatcher
// in memory. It is not an HTTP surface and must not be used for generic profile reads.
type NotificationContactResolver interface {
	ResolveVerifiedContact(ctx context.Context, userID ID, kind string) (NotificationContact, error)
}
