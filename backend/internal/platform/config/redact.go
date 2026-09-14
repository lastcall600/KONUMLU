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
		slog.String("email_provider", c.Email.Provider),
		slog.String("email_ses_region", c.Email.Region),
		slog.Bool("email_ses_from_configured", c.Email.From != ""),
		slog.String("sms_provider", c.SMS.Provider),
		slog.Bool("netgsm_username_configured", c.SMS.Username != ""),
		slog.Bool("netgsm_password_configured", c.SMS.Password != ""),
		slog.Bool("netgsm_msgheader_configured", c.SMS.MsgHeader != ""),
		slog.Bool("push_endpoints_enabled", c.PushEndpoints.Enabled),
		slog.Bool("push_endpoint_encryption_key_configured", len(c.PushEndpoints.EncryptionKey) == 32),
		slog.Bool("push_endpoint_hash_key_configured", len(c.PushEndpoints.HashKey) == 32),
		slog.Bool("webpush_configured", c.WebPush.Wired()),
		slog.Bool("fcm_configured", c.FCM.Wired()),
		slog.Bool("apns_configured", c.APNs.Wired()),
		slog.String("apns_environment", c.APNs.Environment),
		slog.Bool("tr_compliance_ingress_enabled", c.TRCompliance.IngressEnabled),
		slog.Int("tr_compliance_trusted_keys", len(c.TRCompliance.TrustedKeyIDs)),
		slog.Bool("tr_compliance_token_configured", c.TRCompliance.IngressToken != ""),
	)
}

func (p Pool) String() string {
	return fmt.Sprintf("config.Pool{max_conns:%d min_conns:%d}", p.MaxConns, p.MinConns)
}
