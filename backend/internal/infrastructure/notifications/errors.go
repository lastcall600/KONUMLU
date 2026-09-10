package notifications

import (
	"context"
	"errors"

	domain "backend/internal/notifications"
)

var (
	errEmailAdapterRequired = errors.New("email provider adapter required when NOTIFICATIONS_EMAIL_MODE=external")
	errSMSAdapterRequired   = errors.New("sms provider adapter required when NOTIFICATIONS_SMS_MODE=external")
)

// Exported wiring failures for cmd/worker startup checks.
var (
	ErrEmailAdapterRequired = errEmailAdapterRequired
	ErrSMSAdapterRequired   = errSMSAdapterRequired
)

// mapProviderError returns a concise, non-sensitive retryable error.
// Vendor messages, destinations, secrets, credentials, and bodies are dropped.
func mapProviderError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, domain.ErrInvalidDelivery) || errors.Is(err, domain.ErrProviderRequired) ||
		errors.Is(err, domain.ErrUnavailable) {
		return err
	}
	return domain.ErrUnavailable
}
