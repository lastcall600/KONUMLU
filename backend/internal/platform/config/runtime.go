package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment is an explicit process runtime mode.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// Pool is PostgreSQL connection-pool bounds. Zero optional fields mean driver defaults.
type Pool struct {
	ConnectTimeout  time.Duration
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

func (e Environment) String() string {
	return string(e)
}

func (c Config) ProductionLike() bool {
	return c.Environment == EnvStaging || c.Environment == EnvProduction
}

// AuthProviderLaunchBlockers lists AUTH production vendors that are not wired.
// These do not crash the process; they are explicit launch blockers.
func (c Config) AuthProviderLaunchBlockers() []string {
	var out []string
	if !c.HumanChallenge.ProductionWired() {
		out = append(out, "human_challenge_vendor")
	}
	if c.NotificationsEmailMode != NotificationChannelExternal || !c.Email.SESWired() {
		out = append(out, "email_vendor")
	}
	if c.NotificationsSMSMode != NotificationChannelExternal {
		out = append(out, "sms_vendor")
	}
	return out
}

func parseEnvironment(raw string) (Environment, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return EnvDevelopment, nil
	}
	switch Environment(raw) {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
		return Environment(raw), nil
	default:
		return "", fmt.Errorf("%s must be development, test, staging, or production", envAppEnv)
	}
}

func parseLogLevel(raw string) (string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "info", nil
	}
	switch raw {
	case "debug", "info", "warn", "error":
		return raw, nil
	default:
		return "", fmt.Errorf("%s must be debug, info, warn, or error", envLogLevel)
	}
}

func parseTrustedProxies() ([]net.IPNet, bool, error) {
	raw, set := os.LookupEnv(envTrustedProxies)
	if !set {
		return nil, false, nil
	}
	parts := splitCommaList(raw)
	if len(parts) == 0 {
		return nil, true, nil
	}
	out := make([]net.IPNet, 0, len(parts))
	for _, part := range parts {
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				part = fmt.Sprintf("%s/%d", part, bits)
			}
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil || network == nil {
			return nil, true, fmt.Errorf("%s is malformed", envTrustedProxies)
		}
		out = append(out, *network)
	}
	return out, true, nil
}

func parsePool(connectTimeout time.Duration) (Pool, error) {
	p := Pool{ConnectTimeout: connectTimeout}
	if raw := strings.TrimSpace(os.Getenv(envDBMaxConns)); raw != "" {
		n, err := requiredPositiveInt(envDBMaxConns)
		if err != nil {
			return Pool{}, err
		}
		if n > int(^uint32(0)>>1) {
			return Pool{}, fmt.Errorf("%s is invalid", envDBMaxConns)
		}
		p.MaxConns = int32(n)
	}
	if raw := strings.TrimSpace(os.Getenv(envDBMinConns)); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return Pool{}, fmt.Errorf("%s: %w", envDBMinConns, err)
		}
		if n < 0 {
			return Pool{}, fmt.Errorf("%s must not be negative", envDBMinConns)
		}
		p.MinConns = int32(n)
	}
	if p.MaxConns > 0 && p.MinConns > p.MaxConns {
		return Pool{}, fmt.Errorf("%s must be less than or equal to %s", envDBMinConns, envDBMaxConns)
	}
	if raw := strings.TrimSpace(os.Getenv(envDBMaxConnLifetime)); raw != "" {
		d, err := parsePositiveDuration(envDBMaxConnLifetime, raw)
		if err != nil {
			return Pool{}, err
		}
		p.MaxConnLifetime = d
	}
	if raw := strings.TrimSpace(os.Getenv(envDBMaxConnIdleTime)); raw != "" {
		d, err := parsePositiveDuration(envDBMaxConnIdleTime, raw)
		if err != nil {
			return Pool{}, err
		}
		p.MaxConnIdleTime = d
	}
	return p, nil
}

func parseOutboxWorkerConcurrency() (int, error) {
	raw := strings.TrimSpace(os.Getenv(envOutboxWorkerConcurrency))
	if raw == "" {
		return defaultOutboxWorkerConcurrency, nil
	}
	return requiredPositiveInt(envOutboxWorkerConcurrency)
}

func applyRuntimeGates(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if err := gateHumanChallenge(cfg); err != nil {
		return err
	}
	if cfg.AllowInsecureCookies && cfg.ProductionLike() {
		return fmt.Errorf("%s is a development-only setting", envAllowInsecureCookies)
	}
	if cfg.ProductionLike() && !cfg.StaffDevIDP.Empty() {
		return fmt.Errorf("%s is a development-only setting", envStaffDevIDPEnabled)
	}
	if !cfg.ProductionLike() {
		return nil
	}
	if cfg.ValkeyURL == "" {
		return fmt.Errorf("%s must not be empty in %s", envValkeyURL, cfg.Environment)
	}
	if !cfg.TrustedProxiesSet {
		return fmt.Errorf("%s must be set in %s (empty means do not trust X-Forwarded-For)", envTrustedProxies, cfg.Environment)
	}
	if strings.TrimSpace(cfg.WebAuthnRPDisplayName) == "" || strings.TrimSpace(cfg.WebAuthnRPID) == "" || len(cfg.WebAuthnRPOrigins) == 0 {
		return fmt.Errorf("IDENTITY_WEBAUTHN_RP_DISPLAY_NAME, IDENTITY_WEBAUTHN_RP_ID, and IDENTITY_WEBAUTHN_RP_ORIGINS are required in %s", cfg.Environment)
	}
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv(envObjectStorageEnabled)))
	if enabled != "1" && enabled != "true" && enabled != "yes" && enabled != "enabled" {
		return fmt.Errorf("%s must be true in %s", envObjectStorageEnabled, cfg.Environment)
	}
	if err := requireProductionObjectStorage(cfg.Environment); err != nil {
		return err
	}
	if raw, set := os.LookupEnv(envOutboxWorkerConcurrency); !set || strings.TrimSpace(raw) == "" {
		return fmt.Errorf("%s must be set in %s", envOutboxWorkerConcurrency, cfg.Environment)
	}
	return nil
}

func gateHumanChallenge(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.HumanChallenge.Provider))
	if provider == "" {
		provider = "none"
	}
	required := len(cfg.HumanChallenge.Operations) > 0
	if cfg.ProductionLike() && provider == "fake" {
		return fmt.Errorf("%s=fake is a development-only setting", envHumanChallengeProvider)
	}
	if required && provider == "none" {
		return fmt.Errorf("%s cannot be none when %s is set", envHumanChallengeProvider, envHumanChallengeOperations)
	}
	if required && cfg.HumanChallenge.ReplayTTL <= 0 {
		return fmt.Errorf("%s must be greater than zero when challenge operations are set", envHumanChallengeReplayTTL)
	}
	if provider == "turnstile" {
		if strings.TrimSpace(cfg.HumanChallenge.TurnstileSecret) == "" {
			return fmt.Errorf("%s is required when provider is turnstile", envTurnstileSecret)
		}
		if err := validateTurnstileHostnames(cfg.HumanChallenge.AllowedHostnames, true); err != nil {
			return err
		}
		if cfg.HumanChallenge.Timeout <= 0 {
			return fmt.Errorf("%s must be greater than zero when provider is turnstile", envHumanChallengeTimeout)
		}
	}
	if strings.Contains(fmt.Sprintf("%v", cfg.HumanChallenge), cfg.HumanChallenge.TurnstileSecret) && strings.TrimSpace(cfg.HumanChallenge.TurnstileSecret) != "" {
		return fmt.Errorf("%s must not appear in config stringers", envTurnstileSecret)
	}
	return nil
}

func validateTurnstileHostnames(hosts []string, required bool) error {
	cleaned := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		h := strings.TrimSpace(raw)
		if h == "" {
			continue
		}
		if strings.Contains(h, "*") {
			return fmt.Errorf("%s must be an exact hostname allowlist (no wildcards)", envHumanChallengeHostname)
		}
		if strings.ContainsAny(h, " \t:/?\\") || strings.Contains(h, "://") {
			return fmt.Errorf("%s must contain hostnames only", envHumanChallengeHostname)
		}
		cleaned = append(cleaned, h)
	}
	if required && len(cleaned) == 0 {
		return fmt.Errorf("%s must be a non-empty exact hostname allowlist when Turnstile is enabled", envHumanChallengeHostname)
	}
	return nil
}

func requireProductionObjectStorage(env Environment) error {
	if strings.TrimSpace(os.Getenv("OBJECT_STORAGE_ENDPOINT")) == "" {
		return fmt.Errorf("OBJECT_STORAGE_ENDPOINT must not be empty in %s", env)
	}
	if strings.TrimSpace(os.Getenv("OBJECT_STORAGE_REGION")) == "" {
		return fmt.Errorf("OBJECT_STORAGE_REGION must not be empty in %s", env)
	}
	if strings.TrimSpace(os.Getenv("OBJECT_STORAGE_BUCKET")) == "" {
		return fmt.Errorf("OBJECT_STORAGE_BUCKET must not be empty in %s", env)
	}
	source := strings.ToLower(strings.TrimSpace(os.Getenv("OBJECT_STORAGE_CREDENTIAL_SOURCE")))
	if source == "" {
		source = "static"
	}
	if source != "static" && source != "workload" {
		return fmt.Errorf("OBJECT_STORAGE_CREDENTIAL_SOURCE must be static or workload")
	}
	access := strings.TrimSpace(os.Getenv("OBJECT_STORAGE_ACCESS_KEY"))
	secret := strings.TrimSpace(os.Getenv("OBJECT_STORAGE_SECRET_KEY"))
	if source == "static" {
		if access == "" || secret == "" {
			return fmt.Errorf("OBJECT_STORAGE_ACCESS_KEY and OBJECT_STORAGE_SECRET_KEY must not be empty in %s", env)
		}
	}
	if source == "workload" && (access != "" || secret != "") {
		return fmt.Errorf("OBJECT_STORAGE_ACCESS_KEY and OBJECT_STORAGE_SECRET_KEY must be empty when OBJECT_STORAGE_CREDENTIAL_SOURCE=workload")
	}
	return nil
}
