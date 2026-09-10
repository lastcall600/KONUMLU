package notifications

import (
	"context"

	domain "backend/internal/notifications"
)

// EmailClient is a vendor-specific email implementation registered at wiring.
// It must not be imported by the Notifications domain.
type EmailClient interface {
	Send(ctx context.Context, req domain.EmailSendRequest) (domain.SendResult, error)
}

// EmailAdapter implements domain.EmailSender.
//
// Adapter contract:
//   - Notifications passes a stable delivery idempotency key (the delivery ID).
//   - When the vendor supports idempotency keys, the client MUST send that key.
//   - Otherwise the client must be retry-safe as far as the vendor allows.
//   - Semantics are at-least-once. This adapter does not claim exactly-once.
//   - Never log destination, OTP/token, provider credentials, or full
//     request/response bodies.
type EmailAdapter struct {
	client EmailClient
}

func NewEmailAdapter(client EmailClient) (*EmailAdapter, error) {
	if client == nil {
		return nil, errEmailAdapterRequired
	}
	return &EmailAdapter{client: client}, nil
}

func (a *EmailAdapter) Send(ctx context.Context, req domain.EmailSendRequest) (domain.SendResult, error) {
	if a == nil || a.client == nil {
		return domain.SendResult{}, errEmailAdapterRequired
	}
	if req.IdempotencyKey == "" {
		return domain.SendResult{}, domain.ErrInvalidDelivery
	}
	res, err := a.client.Send(ctx, req)
	if err != nil {
		return domain.SendResult{}, mapProviderError(err)
	}
	return res, nil
}

var _ domain.EmailSender = (*EmailAdapter)(nil)
