package apns

import (
	"fmt"
	"strings"
	"time"
)

const (
	ProviderName   = "apns"
	defaultTimeout = 5 * time.Second
	maxTimeout     = 30 * time.Second

	EnvSandbox    = "sandbox"
	EnvProduction = "production"

	hostSandbox    = "https://api.sandbox.push.apple.com"
	hostProduction = "https://api.push.apple.com"

	jwtTTL = 50 * time.Minute
)

// Config is APNs token-authentication settings. The signing key is never logged.
type Config struct {
	TeamID      string
	KeyID       string
	Topic       string
	PrivateKey  string
	Environment string
	Timeout     time.Duration
}

func (c Config) String() string {
	return fmt.Sprintf("apns.Config{team_id_configured:%t key_id_configured:%t topic_configured:%t private_key_configured:%t environment:%s timeout:%s}",
		c.TeamID != "", c.KeyID != "", c.Topic != "", c.PrivateKey != "", c.Environment, c.Timeout)
}

func (c Config) GoString() string { return c.String() }

func (c Config) normalized() Config {
	c.TeamID = strings.TrimSpace(c.TeamID)
	c.KeyID = strings.TrimSpace(c.KeyID)
	c.Topic = strings.TrimSpace(c.Topic)
	c.PrivateKey = strings.TrimSpace(c.PrivateKey)
	c.Environment = strings.ToLower(strings.TrimSpace(c.Environment))
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return c
}

func (c Config) Validate() error {
	c = c.normalized()
	if c.TeamID == "" {
		return fmt.Errorf("APNS_TEAM_ID must not be empty when APNs is enabled")
	}
	if strings.ContainsAny(c.TeamID, " \t\r\n") {
		return fmt.Errorf("APNS_TEAM_ID is malformed")
	}
	if c.KeyID == "" {
		return fmt.Errorf("APNS_KEY_ID must not be empty when APNs is enabled")
	}
	if strings.ContainsAny(c.KeyID, " \t\r\n") {
		return fmt.Errorf("APNS_KEY_ID is malformed")
	}
	if c.Topic == "" {
		return fmt.Errorf("APNS_TOPIC must not be empty when APNs is enabled")
	}
	if strings.ContainsAny(c.Topic, " \t\r\n") {
		return fmt.Errorf("APNS_TOPIC is malformed")
	}
	if c.PrivateKey == "" {
		return fmt.Errorf("APNS_PRIVATE_KEY must not be empty when APNs is enabled")
	}
	switch c.Environment {
	case EnvSandbox, EnvProduction:
	default:
		return fmt.Errorf("APNS_ENVIRONMENT must be sandbox or production")
	}
	if c.Timeout > maxTimeout {
		return fmt.Errorf("APNS_TIMEOUT must be %s or less", maxTimeout)
	}
	return nil
}

func hostFor(env string) string {
	if env == EnvSandbox {
		return hostSandbox
	}
	return hostProduction
}
