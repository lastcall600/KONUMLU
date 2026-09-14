// Package webpush is the standards-based Web Push (RFC 8030 / RFC 8291 / VAPID) transport.
//
// Provider accept (HTTP 201/200) means the push service queued the message.
// It does not mean displayed, read, or delivered to a user. One HTTP call per
// Send. Retry is owned by the Notifications dispatcher.
package webpush

import (
	"context"
	"net/http"
	"time"

	lib "github.com/SherClockHolmes/webpush-go"

	"backend/internal/infrastructure/push"
	domain "backend/internal/notifications"
	"backend/internal/platform/observability"
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Transport sends one Web Push request per invocation.
type Transport struct {
	publicKey  string
	privateKey string
	subject    string
	timeout    time.Duration
	client     doer
}

func (t Transport) String() string   { return "webpush.Transport" }
func (t Transport) GoString() string { return "webpush.Transport{}" }

// New constructs a bounded stdlib HTTP client. VAPID private key is never logged.
func New(cfg Config) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.normalized()
	return NewWithHTTP(cfg, &http.Client{Timeout: cfg.Timeout})
}

// NewWithHTTP constructs a transport against an injected HTTP client (tests).
func NewWithHTTP(cfg Config, client doer) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	cfg = cfg.normalized()
	return &Transport{
		publicKey:  cfg.PublicKey,
		privateKey: cfg.PrivateKey,
		subject:    cfg.Subject,
		timeout:    cfg.Timeout,
		client:     client,
	}, nil
}

func (t *Transport) Send(ctx context.Context, req domain.PushSendRequest) (domain.SendResult, error) {
	if t == nil || t.client == nil || t.privateKey == "" {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.SendResult{}, classifyTransportError(err)
	}
	if req.Provider != domain.PushProviderWebPush || req.Material.Web == nil {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}
	body, err := push.PublicJSON(req.Payload)
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	start := time.Now()
	resp, err := lib.SendNotificationWithContext(callCtx, body, &lib.Subscription{
		Endpoint: req.Material.Web.Endpoint,
		Keys: lib.Keys{
			Auth:   req.Material.Web.Auth,
			P256dh: req.Material.Web.P256dh,
		},
	}, &lib.Options{
		HTTPClient:      t.client,
		Subscriber:      t.subject,
		TTL:             3600,
		VAPIDPublicKey:  t.publicKey,
		VAPIDPrivateKey: t.privateKey,
	})
	if err != nil {
		mapped := classifyTransportError(err)
		class, _, _ := domain.ClassifyProviderError(mapped)
		observability.FromContext(ctx).Info("provider_call",
			"provider", ProviderName,
			"channel_code", string(req.Channel),
			"ok", false,
			"error_class", class,
			"provider_latency_ms", time.Since(start).Milliseconds(),
		)
		return domain.SendResult{}, mapped
	}
	if resp != nil {
		drainBody(resp.Body)
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	mapped := classifyHTTPStatus(status)
	ok := mapped == nil
	class := ""
	if mapped != nil {
		class, _, _ = domain.ClassifyProviderError(mapped)
	}
	observability.FromContext(ctx).Info("provider_call",
		"provider", ProviderName,
		"channel_code", string(req.Channel),
		"ok", ok,
		"error_class", class,
		"provider_latency_ms", time.Since(start).Milliseconds(),
	)
	if mapped != nil {
		return domain.SendResult{}, mapped
	}
	return domain.SendResult{}, nil
}

var _ domain.PushSender = (*Transport)(nil)
