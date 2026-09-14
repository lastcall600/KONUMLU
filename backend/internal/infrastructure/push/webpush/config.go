package webpush

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderName   = "webpush"
	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second
)

// Config is VAPID settings. The private key must never be logged.
type Config struct {
	PublicKey  string
	PrivateKey string
	Subject    string
	Timeout    time.Duration
}

func (c Config) String() string {
	return fmt.Sprintf("webpush.Config{public_key_configured:%t private_key_configured:%t subject_configured:%t timeout:%s}",
		c.PublicKey != "", c.PrivateKey != "", c.Subject != "", c.Timeout)
}

func (c Config) GoString() string { return c.String() }

func (c Config) normalized() Config {
	c.PublicKey = strings.TrimSpace(c.PublicKey)
	c.PrivateKey = strings.TrimSpace(c.PrivateKey)
	c.Subject = strings.TrimSpace(c.Subject)
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	c = c.normalized()
	if c.PublicKey == "" {
		return fmt.Errorf("WEBPUSH_VAPID_PUBLIC_KEY must not be empty when Web Push is enabled")
	}
	if strings.ContainsAny(c.PublicKey, " \t\r\n") {
		return fmt.Errorf("WEBPUSH_VAPID_PUBLIC_KEY is malformed")
	}
	if c.PrivateKey == "" {
		return fmt.Errorf("WEBPUSH_VAPID_PRIVATE_KEY must not be empty when Web Push is enabled")
	}
	if strings.ContainsAny(c.PrivateKey, " \t\r\n") {
		return fmt.Errorf("WEBPUSH_VAPID_PRIVATE_KEY is malformed")
	}
	if err := validateSubject(c.Subject); err != nil {
		return err
	}
	if c.Timeout > maxTimeout {
		return fmt.Errorf("WEBPUSH_TIMEOUT must be %s or less", maxTimeout)
	}
	return nil
}

func validateSubject(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("WEBPUSH_VAPID_SUBJECT must not be empty when Web Push is enabled")
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return fmt.Errorf("WEBPUSH_VAPID_SUBJECT is malformed")
	}
	if strings.HasPrefix(raw, "mailto:") {
		if !strings.Contains(raw[7:], "@") || len(raw) < 10 {
			return fmt.Errorf("WEBPUSH_VAPID_SUBJECT is malformed")
		}
		return nil
	}
	if strings.HasPrefix(raw, "https://") && len(raw) > len("https://") {
		return nil
	}
	return fmt.Errorf("WEBPUSH_VAPID_SUBJECT must be a mailto: or https: URI")
}
