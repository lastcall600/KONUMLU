package config

import (
	"fmt"
	"log/slog"
)

func (c Config) String() string {
	return fmt.Sprintf("config.Config{env:%s http:%s log:%s}", c.Environment, c.HTTPAddr, c.LogLevel)
}

func (c Config) GoString() string {
	return c.String()
}

func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", string(c.Environment)),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("log_level", c.LogLevel),
		slog.Int("outbox_workers", c.OutboxWorkerConcurrency),
		slog.Int("trusted_proxies", len(c.TrustedProxies)),
		slog.Bool("staff_idp_configured", c.StaffIDP.Complete()),
		slog.Bool("staff_dev_idp_enabled", c.StaffDevIDP.Enabled),
		slog.String("human_challenge_provider", c.HumanChallenge.Provider),
		slog.Int("human_challenge_operations", len(c.HumanChallenge.Operations)),
		slog.Int("human_challenge_hostnames", len(c.HumanChallenge.AllowedHostnames)),
		slog.Bool("turnstile_sitekey_configured", c.HumanChallenge.TurnstileSiteKey != ""),
		slog.Bool("turnstile_secret_configured", c.HumanChallenge.TurnstileSecret != ""),
	)
}

func (p Pool) String() string {
	return fmt.Sprintf("config.Pool{max_conns:%d min_conns:%d}", p.MaxConns, p.MinConns)
}
