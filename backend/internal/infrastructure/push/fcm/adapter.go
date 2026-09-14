// Package fcm is the Firebase Cloud Messaging HTTP v1 transport.
//
// Accept means FCM accepted/queued the message. It does not mean displayed
// or delivered to a device. One HTTP call per Send. No adapter retry loop.
package fcm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"backend/internal/infrastructure/push"
	domain "backend/internal/notifications"
	"backend/internal/platform/observability"
)

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

type tokenSource interface {
	Token() (*oauth2.Token, error)
}

// Transport sends one FCM HTTP v1 messages:send request per invocation.
type Transport struct {
	projectID string
	timeout   time.Duration
	tokens    tokenSource
	client    doer
	baseURL   string
}

func (t Transport) String() string   { return "fcm.Transport" }
func (t Transport) GoString() string { return "fcm.Transport{}" }

// New loads Google application default credentials or an optional service-account file.
// File contents and access tokens are never logged or stored on Config.
func New(ctx context.Context, cfg Config) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.normalized()
	if ctx == nil {
		ctx = context.Background()
	}
	src, err := loadTokenSource(ctx, cfg.CredentialsFile)
	if err != nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return NewWithHTTP(cfg, src, &http.Client{Timeout: cfg.Timeout})
}

// NewWithHTTP constructs a transport against injected credentials and HTTP (tests).
func NewWithHTTP(cfg Config, tokens tokenSource, client doer) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if tokens == nil || client == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	cfg = cfg.normalized()
	return &Transport{
		projectID: cfg.ProjectID,
		timeout:   cfg.Timeout,
		tokens:    tokens,
		client:    client,
		baseURL:   sendURL(cfg.ProjectID),
	}, nil
}

func loadTokenSource(ctx context.Context, credFile string) (tokenSource, error) {
	if credFile != "" {
		raw, err := os.ReadFile(credFile)
		if err != nil {
			return nil, err
		}
		creds, err := google.CredentialsFromJSON(ctx, raw, fcmScope)
		if err != nil {
			return nil, err
		}
		return creds.TokenSource, nil
	}
	creds, err := google.FindDefaultCredentials(ctx, fcmScope)
	if err != nil || creds == nil || creds.TokenSource == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	return creds.TokenSource, nil
}

type fcmMessage struct {
	Message struct {
		Token string            `json:"token"`
		Data  map[string]string `json:"data,omitempty"`
	} `json:"message"`
}

func (t *Transport) Send(ctx context.Context, req domain.PushSendRequest) (domain.SendResult, error) {
	if t == nil || t.client == nil || t.tokens == nil || t.projectID == "" {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.SendResult{}, classifyTransportError(err)
	}
	if req.Provider != domain.PushProviderFCM || req.Material.Mobile == nil || strings.TrimSpace(req.Material.Mobile.Token) == "" {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	tok, err := t.tokens.Token()
	if err != nil || tok == nil || strings.TrimSpace(tok.AccessToken) == "" {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	var envelope fcmMessage
	envelope.Message.Token = req.Material.Mobile.Token
	envelope.Message.Data = push.PublicMap(req.Payload)
	body, err := json.Marshal(envelope)
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL, bytes.NewReader(body))
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderRetryable
	}
	httpReq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
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
	var accepted struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &accepted)
	return domain.SendResult{ProviderRef: strings.TrimSpace(accepted.Name)}, nil
}

var _ domain.PushSender = (*Transport)(nil)
