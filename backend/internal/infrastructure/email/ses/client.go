package ses

import (
	"context"

	domain "backend/internal/notifications"
)

// VerificationClient adapts Transport to the Identity/verification EmailClient port.
type VerificationClient struct {
	transport *Transport
}

func NewVerificationClient(t *Transport) (*VerificationClient, error) {
	if t == nil || t.api == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return &VerificationClient{transport: t}, nil
}

func (c *VerificationClient) Send(ctx context.Context, req domain.EmailSendRequest) (domain.SendResult, error) {
	if c == nil || c.transport == nil {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	body, err := renderVerification(req)
	if err != nil {
		return domain.SendResult{}, err
	}
	return c.transport.deliver(ctx, outbound{
		Recipient:      req.Destination,
		Subject:        body.Subject,
		Text:           body.Text,
		HTML:           body.HTML,
		IdempotencyKey: req.IdempotencyKey,
	})
}

// ChannelClient adapts Transport to the Notifications ChannelSender port.
// Consent, preference, account-state, and suppression stay in the dispatcher.
type ChannelClient struct {
	transport *Transport
}

func NewChannelClient(t *Transport) (*ChannelClient, error) {
	if t == nil || t.api == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return &ChannelClient{transport: t}, nil
}

func (c *ChannelClient) Send(ctx context.Context, req domain.ChannelSendRequest) (domain.SendResult, error) {
	if c == nil || c.transport == nil {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	body, err := renderChannel(req)
	if err != nil {
		return domain.SendResult{}, err
	}
	return c.transport.deliver(ctx, outbound{
		Recipient:      req.Destination,
		Subject:        body.Subject,
		Text:           body.Text,
		HTML:           body.HTML,
		IdempotencyKey: req.IdempotencyKey,
	})
}

var _ domain.ChannelSender = (*ChannelClient)(nil)
