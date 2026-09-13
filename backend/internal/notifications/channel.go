package notifications

import (
	"context"
	"errors"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/notifications/policy"
)

var (
	errProviderRetryable    = errors.New("notification provider retryable")
	errProviderPermanent    = errors.New("notification provider permanent")
	errProviderTimeout      = errors.New("notification provider timeout")
	errProviderUnconfigured = errors.New("notification provider unconfigured")
)

var (
	ErrProviderRetryable    = errProviderRetryable
	ErrProviderPermanent    = errProviderPermanent
	ErrProviderTimeout      = errProviderTimeout
	ErrProviderUnconfigured = errProviderUnconfigured
)

const (
	ErrorClassRetryable    = "retryable"
	ErrorClassPermanent    = "permanent"
	ErrorClassTimeout      = "timeout"
	ErrorClassUnconfigured = "unconfigured"
)

// ChannelSendRequest is provider input for general notifications. It must never be logged.
// It is not verification OTP delivery.
type ChannelSendRequest struct {
	Channel        policy.Channel
	Destination    string
	TemplateKey    string
	Locale         string
	Variables      map[string]string
	IdempotencyKey string
}

func (ChannelSendRequest) String() string { return "notifications.ChannelSendRequest" }
func (ChannelSendRequest) GoString() string {
	return "notifications.ChannelSendRequest{}"
}

func (r ChannelSendRequest) valid() bool {
	if r.Destination == "" || r.TemplateKey == "" || r.IdempotencyKey == "" {
		return false
	}
	switch r.Channel {
	case policy.ChannelEmail, policy.ChannelSMS, policy.ChannelWebPush, policy.ChannelMobilePush:
		return true
	default:
		return false
	}
}

type ChannelSender interface {
	Send(ctx context.Context, req ChannelSendRequest) (SendResult, error)
}

type DispatcherConfig struct {
	BatchSize      int
	ProcessingHold time.Duration
	PollInterval   time.Duration
}

func (c DispatcherConfig) normalized() DispatcherConfig {
	if c.BatchSize <= 0 {
		c.BatchSize = 10
	}
	if c.ProcessingHold <= 0 {
		c.ProcessingHold = 30 * time.Second
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	return c
}

func ClassifyProviderError(err error) (errorClass string, retryable, giveUp bool) {
	if err == nil {
		return "", false, false
	}
	if errors.Is(err, context.Canceled) {
		return ErrorClassRetryable, true, false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, errProviderTimeout) {
		return ErrorClassTimeout, true, false
	}
	if errors.Is(err, errProviderPermanent) {
		return ErrorClassPermanent, false, true
	}
	if errors.Is(err, errProviderUnconfigured) {
		return ErrorClassUnconfigured, true, false
	}
	if errors.Is(err, errProviderRetryable) || errors.Is(err, errUnavailable) || errors.Is(err, errProviderRequired) {
		return ErrorClassRetryable, true, false
	}
	return ErrorClassRetryable, true, false
}

func contactKind(ch policy.Channel) (string, bool) {
	switch ch {
	case policy.ChannelEmail:
		return identitycontracts.NotificationContactEmail, true
	case policy.ChannelSMS:
		return identitycontracts.NotificationContactPhone, true
	default:
		return "", false
	}
}
