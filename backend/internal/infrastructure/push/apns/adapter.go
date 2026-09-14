// Package apns is the Apple Push Notification service HTTP/2 token-auth transport.
//
// HTTP 200 means APNs accepted the notification. It does not mean displayed
// or delivered. One HTTP/2 request per Send. No adapter retry loop.
package apns

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/http2"

	domain "backend/internal/notifications"
	"backend/internal/platform/observability"
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Transport sends one APNs request per invocation.
type Transport struct {
	topic   string
	host    string
	timeout time.Duration
	jwt     *jwtCache
	client  doer
}

func (t Transport) String() string   { return "apns.Transport" }
func (t Transport) GoString() string { return "apns.Transport{}" }

// New constructs an HTTP/2 client and parses the .p8 signing key.
func New(cfg Config) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.normalized()
	key, err := parseAPNsKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true,
	}
	if err := http2.ConfigureTransport(tr); err != nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return NewWithHTTP(cfg, key, &http.Client{Timeout: cfg.Timeout, Transport: tr}, nil)
}

// NewWithHTTP constructs a transport against an injected client, key, and optional clock (tests).
func NewWithHTTP(cfg Config, key *ecdsa.PrivateKey, client doer, now func() time.Time) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if key == nil || client == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	cfg = cfg.normalized()
	return &Transport{
		topic:   cfg.Topic,
		host:    hostFor(cfg.Environment),
		timeout: cfg.Timeout,
		jwt:     &jwtCache{key: key, keyID: cfg.KeyID, teamID: cfg.TeamID, now: now},
		client:  client,
	}, nil
}

func (t *Transport) Send(ctx context.Context, req domain.PushSendRequest) (domain.SendResult, error) {
	if t == nil || t.client == nil || t.jwt == nil || t.topic == "" {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.SendResult{}, classifyTransportError(err)
	}
	if req.Provider != domain.PushProviderAPNs || req.Material.Mobile == nil || strings.TrimSpace(req.Material.Mobile.Token) == "" {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	bearer, err := t.jwt.bearer()
	if err != nil {
		return domain.SendResult{}, err
	}
	envelope, err := json.Marshal(struct {
		Aps struct {
			ContentAvailable int `json:"content-available"`
		} `json:"aps"`
		Category    string `json:"category,omitempty"`
		TemplateKey string `json:"template_key,omitempty"`
		ReferenceID string `json:"reference_id,omitempty"`
	}{
		Category:    req.Payload.Category,
		TemplateKey: req.Payload.TemplateKey,
		ReferenceID: req.Payload.ReferenceID,
	})
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	url := t.host + "/3/device/" + strings.TrimSpace(req.Material.Mobile.Token)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(envelope))
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderRetryable
	}
	httpReq.Header.Set("Authorization", "bearer "+bearer)
	httpReq.Header.Set("apns-topic", t.topic)
	httpReq.Header.Set("apns-push-type", "background")
	httpReq.Header.Set("apns-priority", "5")
	httpReq.Header.Set("Content-Type", "application/json")

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	httpReq = httpReq.WithContext(callCtx)

	start := time.Now()
	resp, err := t.client.Do(httpReq)
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
	defer resp.Body.Close()
	raw := drainLimited(resp.Body)
	mapped := classifyHTTP(resp.StatusCode, raw)
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
	ref := ""
	if resp != nil {
		ref = strings.TrimSpace(resp.Header.Get("apns-id"))
	}
	return domain.SendResult{ProviderRef: ref}, nil
}

var _ domain.PushSender = (*Transport)(nil)
