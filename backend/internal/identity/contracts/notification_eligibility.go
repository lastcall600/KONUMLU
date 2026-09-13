package contracts

import "context"

var (
	ErrNotificationEligibilityUnavailable = ErrUnavailable
	ErrNotificationUserNotFound           = ErrNotFound
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
