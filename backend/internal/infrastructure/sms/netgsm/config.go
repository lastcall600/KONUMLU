// Package netgsm is the Netgsm transactional and OTP SMS transport.
//
// It is not notification policy, Identity, Email/SES, Push, Staff IAM, Trust,
// Turnstile, or the Türkiye Compliance Gateway. A successful send/otp response
// means Netgsm accepted/queued the message. It does not prove handset delivery.
// Netgsm job IDs are opaque strings. Submission is at-least-once under
// ambiguous failures; the Notifications dispatcher / outbox owns retry.
//
// This package does not send marketing SMS, does not invent İYS/iysfilter
// policy, and never generates OTP codes.
package netgsm

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	ProviderName = "netgsm"
	ChannelSMS   = "sms"

	defaultBaseURL = "https://api.netgsm.com.tr"
	pathSend       = "/sms/rest/v2/send"
	pathOTP        = "/sms/rest/v2/otp"

	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second

	maxOTPRunes           = 160
	maxTransactionalRunes = 917
)

// Config holds Netgsm transport settings. Password must never be logged.
type Config struct {
	Username  string
	Password  string
	MsgHeader string
	Timeout   time.Duration
	BaseURL   string
}

func (c Config) String() string {
	return fmt.Sprintf("netgsm.Config{username_configured:%t password_configured:%t msgheader_configured:%t timeout:%s}",
		c.Username != "", c.Password != "", c.MsgHeader != "", c.Timeout)
}

func (c Config) GoString() string { return c.String() }

func (c Config) normalized() Config {
	c.Username = strings.TrimSpace(c.Username)
	c.Password = strings.TrimSpace(c.Password)
	c.MsgHeader = strings.TrimSpace(c.MsgHeader)
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = defaultBaseURL
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	c = c.normalized()
	if c.Username == "" {
		return fmt.Errorf("NETGSM_USERNAME must not be empty when SMS_PROVIDER=netgsm")
	}
	if strings.ContainsAny(c.Username, " \t\r\n") {
		return fmt.Errorf("NETGSM_USERNAME is malformed")
	}
	if c.Password == "" {
		return fmt.Errorf("NETGSM_PASSWORD must not be empty when SMS_PROVIDER=netgsm")
	}
	if strings.ContainsAny(c.Password, "\r\n") {
		return fmt.Errorf("NETGSM_PASSWORD is malformed")
	}
	if err := validateMsgHeader(c.MsgHeader); err != nil {
		return err
	}
	if c.Timeout > maxTimeout {
		return fmt.Errorf("NETGSM_TIMEOUT must be %s or less", maxTimeout)
	}
	return nil
}

func validateMsgHeader(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("NETGSM_MSGHEADER must not be empty when SMS_PROVIDER=netgsm")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("NETGSM_MSGHEADER is malformed")
	}
	for _, r := range raw {
		if unicode.IsSpace(r) && r != ' ' {
			return fmt.Errorf("NETGSM_MSGHEADER is malformed")
		}
	}
	return nil
}
