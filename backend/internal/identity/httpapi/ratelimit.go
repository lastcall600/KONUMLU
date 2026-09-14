package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend/internal/identity"
	"backend/internal/platform/observability"
)

const unknownClientIP = "unknown"

// incrementer is the Valkey atomic counter used for disposable rate-limit windows.
type incrementer interface {
	Increment(ctx context.Context, key string, ttl time.Duration) (int64, error)
}

// ClientIPFunc extracts a client IP for the IP bucket.
// Untrusted requests use RemoteAddr only. Trusted-proxy CIDRs are injected
// from process config; X-Forwarded-For is never trusted blindly.
type ClientIPFunc func(*http.Request) string

// AuthRateLimit is injected from process config. Production numbers are not hardcoded.
type AuthRateLimit struct {
	IPMaxAttempts           int
	IPWindow                time.Duration
	PasswordUserMaxAttempts int
	PasswordUserWindow      time.Duration
	TargetMaxAttempts       int
	TargetWindow            time.Duration
	CompleteMaxAttempts     int
	CompleteWindow          time.Duration
	SensitiveMaxAttempts    int
	SensitiveWindow         time.Duration
	Challenge               identity.HumanChallengePolicy
	Verifier                identity.HumanChallenge
}

func (p AuthRateLimit) policy() (identity.AuthAbusePolicy, error) {
	ip := identity.RateLimitBucket{Max: p.IPMaxAttempts, Window: p.IPWindow}
	account := identity.RateLimitBucket{Max: p.PasswordUserMaxAttempts, Window: p.PasswordUserWindow}
	target := account
	if p.TargetMaxAttempts > 0 {
		target = identity.RateLimitBucket{Max: p.TargetMaxAttempts, Window: p.TargetWindow}
	}
	complete := ip
	if p.CompleteMaxAttempts > 0 {
		complete = identity.RateLimitBucket{Max: p.CompleteMaxAttempts, Window: p.CompleteWindow}
	}
	sensitive := account
	if p.SensitiveMaxAttempts > 0 {
		sensitive = identity.RateLimitBucket{Max: p.SensitiveMaxAttempts, Window: p.SensitiveWindow}
	}
	return identity.BuildAuthAbusePolicy(ip, account, target, complete, sensitive)
}

// AbuseGuard applies Identity HTTP auth rate limits and challenge policy.
type AbuseGuard struct {
	engine *identity.AbuseEngine
	ip     ClientIPFunc
}

func NewAbuseGuard(inc incrementer, policy AuthRateLimit, ip ClientIPFunc) (*AbuseGuard, error) {
	if inc == nil {
		return nil, identity.ErrUnavailable
	}
	abusePolicy, err := policy.policy()
	if err != nil {
		return nil, err
	}
	engine, err := identity.NewAbuseEngine(inc, abusePolicy, policy.Challenge, policy.Verifier)
	if err != nil {
		return nil, err
	}
	if ip == nil {
		ip = ClientIPFromRemoteAddr
	}
	return &AbuseGuard{engine: engine, ip: ip}, nil
}

// ClientIPFromRemoteAddr uses net/http RemoteAddr only.
func ClientIPFromRemoteAddr(r *http.Request) string {
	if r == nil {
		return unknownClientIP
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	addr = strings.Trim(addr, "[]")
	if addr == "" {
		return unknownClientIP
	}
	return addr
}

func (g *AbuseGuard) clientIP(r *http.Request) string {
	if g == nil || g.ip == nil {
		return ClientIPFromRemoteAddr(r)
	}
	return g.ip(r)
}

func (g *AbuseGuard) evaluate(ctx context.Context, sub identity.AbuseSubject, phase identity.AbusePhase) identity.RiskOutcome {
	if g == nil || g.engine == nil {
		return identity.RiskOutcome{Decision: identity.RiskRestrict, Reason: identity.ReasonStorageUnavailable, Operation: sub.Operation}
	}
	return g.engine.Evaluate(ctx, sub, phase)
}

func requestHostname(r *http.Request) string {
	if r == nil {
		return ""
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" && origin != "null" {
		if u, err := url.Parse(origin); err == nil {
			if host := strings.TrimSpace(u.Hostname()); host != "" {
				return host
			}
		}
	}
	return strings.TrimSpace(r.Host)
}

func logAuthRisk(ctx context.Context, out identity.RiskOutcome) {
	if out.Allow() && !out.Challenged {
		return
	}
	observability.FromContext(ctx).Info("auth_abuse",
		"auth_operation", string(out.Operation),
		"risk_decision", string(out.Decision),
		"reason_code", string(out.Reason),
		"rate_limit_dimension", string(out.Dimension),
		"challenged", out.Challenged,
		"challenge_provider", out.Provider,
		"challenge_ok", out.ChallengeOK,
		"error_class", riskErrorClass(out),
	)
}

func riskErrorClass(out identity.RiskOutcome) string {
	if out.Allow() {
		return ""
	}
	switch out.Reason {
	case identity.ReasonVelocityIP, identity.ReasonVelocityAccount, identity.ReasonVelocityTarget:
		return "rate_limited"
	case identity.ReasonStorageUnavailable, identity.ReasonProviderUnavailable:
		return "unavailable"
	case identity.ReasonChallengeRequired:
		return "challenge_required"
	default:
		return "forbidden"
	}
}
