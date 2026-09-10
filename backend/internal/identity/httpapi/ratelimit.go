package httpapi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"backend/internal/identity"
)

var errRateLimited = errors.New("rate limited")

const (
	authIPKeyPrefix           = "identity:auth:rl:ip:"
	authPasswordUserKeyPrefix = "identity:auth:rl:password-user:"
	signupVerifyIPKeyPrefix   = "identity:auth:rl:signup-verify-ip:"
	unknownClientIP           = "unknown"
)

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
}

// AbuseGuard applies Identity HTTP auth rate limits. Counters are disposable.
type AbuseGuard struct {
	inc incrementer
	ip  ClientIPFunc
	p   AuthRateLimit
}

func NewAbuseGuard(inc incrementer, policy AuthRateLimit, ip ClientIPFunc) (*AbuseGuard, error) {
	if inc == nil {
		return nil, identity.ErrUnavailable
	}
	if policy.IPMaxAttempts <= 0 || policy.IPWindow <= 0 ||
		policy.PasswordUserMaxAttempts <= 0 || policy.PasswordUserWindow <= 0 {
		return nil, identity.ErrUnavailable
	}
	if ip == nil {
		ip = ClientIPFromRemoteAddr
	}
	return &AbuseGuard{inc: inc, ip: ip, p: policy}, nil
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

func (g *AbuseGuard) allowIP(ctx context.Context, r *http.Request) error {
	if g == nil {
		return identity.ErrUnavailable
	}
	return g.check(ctx, authIPKeyPrefix+g.ip(r), g.p.IPMaxAttempts, g.p.IPWindow)
}

func (g *AbuseGuard) allowPasswordUser(ctx context.Context, userID identity.ID) error {
	if g == nil {
		return identity.ErrUnavailable
	}
	if userID.IsZero() {
		return identity.ErrUnavailable
	}
	return g.check(ctx, authPasswordUserKeyPrefix+userID.String(), g.p.PasswordUserMaxAttempts, g.p.PasswordUserWindow)
}

func (g *AbuseGuard) allowSignupVerifyIP(ctx context.Context, r *http.Request) error {
	if g == nil {
		return identity.ErrUnavailable
	}
	return g.check(ctx, signupVerifyIPKeyPrefix+g.clientIP(r), g.p.IPMaxAttempts, g.p.IPWindow)
}

func (g *AbuseGuard) clientIP(r *http.Request) string {
	if g == nil || g.ip == nil {
		return ClientIPFromRemoteAddr(r)
	}
	return g.ip(r)
}

func (g *AbuseGuard) check(ctx context.Context, key string, max int, window time.Duration) error {
	n, err := g.inc.Increment(ctx, key, window)
	if err != nil {
		return identity.ErrUnavailable
	}
	if n > int64(max) {
		return errRateLimited
	}
	return nil
}
