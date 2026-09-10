package notifications

import (
	"fmt"
	"strings"

	domain "backend/internal/notifications"
)

const (
	modeDisabled = "disabled"
	modeExternal = "external"
)

// Transports are vendor-specific clients registered at the composition root.
// No vendor is selected yet; production wiring leaves these nil.
type Transports struct {
	Email EmailClient
	SMS   SMSClient
}

// Senders are Notifications ports. A nil sender means the channel is not
// deliverable. Callers must fail retryably and must not mark the delivery sent.
type Senders struct {
	Email domain.EmailSender
	SMS   domain.SMSSender
}

// Bind selects channel adapters from explicit modes.
//
// disabled: sender stays nil (events remain retryable/unavailable).
// external: a registered client is required; absence fails process wiring.
// There is no production no-op sender.
func Bind(emailMode, smsMode string, transports Transports) (Senders, error) {
	email, err := bindEmail(emailMode, transports.Email)
	if err != nil {
		return Senders{}, err
	}
	sms, err := bindSMS(smsMode, transports.SMS)
	if err != nil {
		return Senders{}, err
	}
	return Senders{Email: email, SMS: sms}, nil
}

func bindEmail(mode string, client EmailClient) (domain.EmailSender, error) {
	switch normalizeMode(mode) {
	case modeDisabled:
		return nil, nil
	case modeExternal:
		if client == nil {
			return nil, errEmailAdapterRequired
		}
		return NewEmailAdapter(client)
	default:
		return nil, fmt.Errorf("email channel mode must be %s or %s", modeDisabled, modeExternal)
	}
}

func bindSMS(mode string, client SMSClient) (domain.SMSSender, error) {
	switch normalizeMode(mode) {
	case modeDisabled:
		return nil, nil
	case modeExternal:
		if client == nil {
			return nil, errSMSAdapterRequired
		}
		return NewSMSAdapter(client)
	default:
		return nil, fmt.Errorf("sms channel mode must be %s or %s", modeDisabled, modeExternal)
	}
}

func normalizeMode(mode string) string {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return modeDisabled
	}
	return m
}
