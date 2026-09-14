package netgsm

import (
	"context"

	domain "backend/internal/notifications"
)

// OTPClient adapts Transport to the Identity/verification SMSClient port.
// Identity remains responsible for code generation, lifetime, and verification.
type OTPClient struct {
	transport *Transport
}

func NewOTPClient(t *Transport) (*OTPClient, error) {
	if t == nil || t.client == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return &OTPClient{transport: t}, nil
}

func (c *OTPClient) Send(ctx context.Context, req domain.SMSSendRequest) (domain.SendResult, error) {
	if c == nil || c.transport == nil {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	body, err := renderOTP(req)
	if err != nil {
		return domain.SendResult{}, err
	}
	return c.transport.deliver(ctx, outbound{
		kind:    endpointOTP,
		phone:   req.Destination,
		message: body,
	})
}

// ChannelClient adapts Transport to the Notifications ChannelSender port.
// Consent, preference, account-state, and suppression stay in the dispatcher.
type ChannelClient struct {
	transport *Transport
}

func NewChannelClient(t *Transport) (*ChannelClient, error) {
	if t == nil || t.client == nil {
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
		kind:    endpointSend,
		phone:   req.Destination,
		message: body,
	})
}

var _ domain.ChannelSender = (*ChannelClient)(nil)
