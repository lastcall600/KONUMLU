package ses

import (
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode"
)

const (
	ProviderName = "ses"
	ChannelEmail = "email"

	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second
)

// Config is non-secret SES transport settings. Credentials are never stored here.
type Config struct {
	Region  string
	From    string
	Timeout time.Duration
}

func (c Config) String() string {
	return fmt.Sprintf("ses.Config{region:%s from_configured:%t timeout:%s}", c.Region, c.From != "", c.Timeout)
}

func (c Config) GoString() string { return c.String() }

func (c Config) normalized() Config {
	c.Region = strings.TrimSpace(c.Region)
	c.From = strings.TrimSpace(c.From)
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	c = c.normalized()
	if c.Region == "" {
		return fmt.Errorf("EMAIL_SES_REGION must not be empty when EMAIL_PROVIDER=ses")
	}
	if strings.ContainsAny(c.Region, " \t\r\n") {
		return fmt.Errorf("EMAIL_SES_REGION is malformed")
	}
	if err := validateFrom(c.From); err != nil {
		return err
	}
	if c.Timeout > maxTimeout {
		return fmt.Errorf("EMAIL_SES_TIMEOUT must be %s or less", maxTimeout)
	}
	return nil
}

func validateFrom(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("EMAIL_SES_FROM must not be empty when EMAIL_PROVIDER=ses")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("EMAIL_SES_FROM is malformed")
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr == nil || addr.Address == "" {
		return fmt.Errorf("EMAIL_SES_FROM is malformed")
	}
	if strings.Count(addr.Address, "@") != 1 {
		return fmt.Errorf("EMAIL_SES_FROM is malformed")
	}
	return nil
}

func validDestination(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return false
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr == nil {
		return false
	}
	at := strings.LastIndex(addr.Address, "@")
	if at <= 0 || at == len(addr.Address)-1 {
		return false
	}
	for _, r := range addr.Address {
		if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
