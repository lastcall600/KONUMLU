// Package turnstile is the Cloudflare Turnstile HumanChallenge adapter.
//
// V1 Siteverify policy:
//   - one POST to the official endpoint (or a test-injected URL)
//   - secret + response only; remoteip is omitted
//   - no idempotency_key and no retry (tokens are single-use; ambiguous
//     network/provider results fail closed and require a fresh token)
//   - success requires success=true, allowlisted hostname, and server-owned action
//
// This adapter is consumer auth infrastructure. It is not Step-Up, Trust,
// EİDS, or the Türkiye Compliance Gateway.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"backend/internal/identity"
)

const (
	// OfficialSiteverifyURL is Cloudflare's production Siteverify endpoint.
	OfficialSiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

	// MaxTokenLength is Cloudflare's documented token bound (characters).
	MaxTokenLength = 2048

	defaultTimeout     = 3 * time.Second
	maxResponseBytes   = 16 << 10
	providerName       = identity.HumanChallengeProviderTurnstile
	formSecret         = "secret"
	formResponse       = "response"
	formRemoteIP       = "remoteip"
	formIdempotencyKey = "idempotency_key"
)

// Failure classes are internal. They are never returned as Cloudflare error-codes.
const (
	ClassInvalidToken              = "invalid_token"
	ClassExpiredOrDuplicate        = "expired_or_duplicate"
	ClassActionMismatch            = "action_mismatch"
	ClassHostnameMismatch          = "hostname_mismatch"
	ClassProviderUnavailable       = "provider_unavailable"
	ClassMalformedProviderResponse = "malformed_provider_response"
)

// Config is process-injected Turnstile settings. Secret must never be logged.
type Config struct {
	Secret           string
	AllowedHostnames []string
	Timeout          time.Duration
	HTTPClient       *http.Client
	// SiteverifyURL overrides the official endpoint for httptest only.
	SiteverifyURL string
}

// Adapter verifies Turnstile tokens with one bounded Siteverify request.
type Adapter struct {
	secret     string
	allowed    map[string]struct{}
	timeout    time.Duration
	client     *http.Client
	verifyURL  string
}

// New constructs a production-safe adapter. Secret and exact hostnames are required.
func New(cfg Config) (*Adapter, error) {
	secret := strings.TrimSpace(cfg.Secret)
	if secret == "" {
		return nil, fmt.Errorf("turnstile secret is required")
	}
	allowed, err := normalizeAllowlist(cfg.AllowedHostnames)
	if err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else if client.Timeout <= 0 {
		clone := *client
		clone.Timeout = timeout
		client = &clone
	}
	verifyURL := strings.TrimSpace(cfg.SiteverifyURL)
	if verifyURL == "" {
		verifyURL = OfficialSiteverifyURL
	}
	return &Adapter{
		secret:    secret,
		allowed:   allowed,
		timeout:   timeout,
		client:    client,
		verifyURL: verifyURL,
	}, nil
}

func (a *Adapter) Name() string { return providerName }

func (a *Adapter) Verify(ctx context.Context, in identity.HumanChallengeInput) (identity.HumanChallengeResult, error) {
	if a == nil || a.client == nil {
		return identity.HumanChallengeResult{}, unavailable(errAdapter)
	}
	if err := ctx.Err(); err != nil {
		return identity.HumanChallengeResult{}, unavailable(err)
	}
	token := strings.TrimSpace(in.Token)
	expected := strings.TrimSpace(string(in.Action))
	out := identity.HumanChallengeResult{Action: identity.HumanChallengeAction(expected)}
	if token == "" || utf8.RuneCountInString(token) > MaxTokenLength {
		out.FailureClass = ClassInvalidToken
		return out, nil
	}
	if expected == "" {
		out.FailureClass = ClassActionMismatch
		return out, nil
	}

	form := url.Values{}
	form.Set(formSecret, a.secret)
	form.Set(formResponse, token)
	encoded := form.Encode()
	if strings.Contains(encoded, formRemoteIP+"=") || strings.Contains(encoded, formIdempotencyKey+"=") {
		return identity.HumanChallengeResult{}, unavailable(errAdapter)
	}

	reqCtx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, a.verifyURL, strings.NewReader(encoded))
	if err != nil {
		return identity.HumanChallengeResult{}, unavailable(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return identity.HumanChallengeResult{}, unavailable(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return identity.HumanChallengeResult{}, unavailable(err)
	}
	if len(body) > maxResponseBytes {
		return identity.HumanChallengeResult{}, malformed(errOversized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return identity.HumanChallengeResult{}, unavailable(errNon2xx)
	}

	var parsed siteverifyResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return identity.HumanChallengeResult{}, malformed(err)
	}
	host := strings.TrimSpace(parsed.Hostname)
	action := strings.TrimSpace(parsed.Action)
	out.Hostname = host
	out.Action = identity.HumanChallengeAction(action)

	if class := classifyErrorCodes(parsed.ErrorCodes); class != "" && !parsed.Success {
		out.FailureClass = class
		if class == ClassProviderUnavailable {
			return identity.HumanChallengeResult{Action: identity.HumanChallengeAction(expected), FailureClass: class}, unavailable(errProvider)
		}
		return out, nil
	}
	if !parsed.Success {
		out.FailureClass = ClassInvalidToken
		return out, nil
	}
	if host == "" || !a.hostnameAllowed(host) {
		out.FailureClass = ClassHostnameMismatch
		out.OK = false
		return out, nil
	}
	if action != expected {
		out.FailureClass = ClassActionMismatch
		out.OK = false
		return out, nil
	}
	out.OK = true
	out.Action = identity.HumanChallengeAction(expected)
	out.Hostname = host
	return out, nil
}

func (a *Adapter) hostnameAllowed(host string) bool {
	_, ok := a.allowed[strings.ToLower(strings.TrimSpace(host))]
	return ok
}

type siteverifyResponse struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

func classifyErrorCodes(codes []string) string {
	for _, raw := range codes {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "timeout-or-duplicate":
			return ClassExpiredOrDuplicate
		case "internal-error", "invalid-input-secret", "missing-input-secret":
			return ClassProviderUnavailable
		case "invalid-input-response", "missing-input-response", "bad-request":
			return ClassInvalidToken
		}
	}
	return ""
}

func normalizeAllowlist(hosts []string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(hosts))
	for _, raw := range hosts {
		h := strings.ToLower(strings.TrimSpace(raw))
		if h == "" {
			continue
		}
		if strings.ContainsAny(h, " \t:*?/\\") || strings.Contains(h, "..") || strings.HasPrefix(h, ".") || strings.Contains(h, "://") {
			return nil, fmt.Errorf("turnstile hostname allowlist is invalid")
		}
		if strings.Contains(h, "*") {
			return nil, fmt.Errorf("turnstile hostname allowlist must be exact")
		}
		out[h] = struct{}{}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("turnstile hostname allowlist is required")
	}
	return out, nil
}

var (
	errAdapter   = errors.New("turnstile adapter unavailable")
	errNon2xx    = errors.New("turnstile provider status")
	errOversized = errors.New("turnstile provider response too large")
	errProvider  = errors.New("turnstile provider unavailable")
)

type classError struct {
	class string
	err   error
}

func unavailable(err error) error {
	return &classError{class: ClassProviderUnavailable, err: err}
}

func malformed(err error) error {
	return &classError{class: ClassMalformedProviderResponse, err: err}
}

func (e *classError) Error() string {
	if e == nil {
		return "turnstile: provider_unavailable"
	}
	return "turnstile: " + e.class
}

func (e *classError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (e *classError) Class() string {
	if e == nil {
		return ClassProviderUnavailable
	}
	return e.class
}

func FailureClassOf(err error) string {
	var ce *classError
	if errors.As(err, &ce) {
		return ce.Class()
	}
	return ""
}

var _ identity.HumanChallenge = (*Adapter)(nil)
