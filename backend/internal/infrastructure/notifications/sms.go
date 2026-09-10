package notifications

import (
	"context"

	domain "backend/internal/notifications"
)

// SMSClient is a vendor-specific SMS implementation registered at wiring.
// It must not be imported by the Notifications domain.
type SMSClient interface {
	Send(ctx context.Context, req domain.SMSSendRequest) (domain.SendResult, error)
}

// SMSAdapter implements domain.SMSSender.
//
// Adapter contract:
//   - Notifications passes a stable delivery idempotency key (the delivery ID).
//   - When the vendor supports idempotency keys, the client MUST send that key.
//   - Otherwise the client must be retry-safe as far as the vendor allows.
//   - Semantics are at-least-once. This adapter does not claim exactly-once.
//   - Never log destination, OTP/token, provider credentials, or full
//     request/response bodies.
type SMSAdapter struct {
	client SMSClient
}

func NewSMSAdapter(client SMSClient) (*SMSAdapter, error) {
	if client == nil {
		return nil, errSMSAdapterRequired
	}
	return &SMSAdapter{client: client}, nil
}

func (a *SMSAdapter) Send(ctx context.Context, req domain.SMSSendRequest) (domain.SendResult, error) {
	if a == nil || a.client == nil {
		return domain.SendResult{}, errSMSAdapterRequired
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

var _ domain.SMSSender = (*SMSAdapter)(nil)
