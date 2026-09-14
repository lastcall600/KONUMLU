package netgsm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	domain "backend/internal/notifications"
	"backend/internal/platform/observability"
)

const maxResponseBytes = 64 << 10

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Transport sends SMS through Netgsm REST v2. One HTTP call per invocation.
// Retry/backoff is owned by the Notifications dispatcher / Identity outbox.
type Transport struct {
	username  string
	password  string
	msgheader string
	timeout   time.Duration
	baseURL   string
	client    doer
}

// New constructs a bounded stdlib HTTP client. No Netgsm SDK is used.
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
		username:  cfg.Username,
		password:  cfg.Password,
		msgheader: cfg.MsgHeader,
		timeout:   cfg.Timeout,
		baseURL:   cfg.BaseURL,
		client:    client,
	}, nil
}

type outbound struct {
	kind    endpointKind
	phone   string
	message string
}

func (t *Transport) deliver(ctx context.Context, msg outbound) (domain.SendResult, error) {
	if t == nil || t.client == nil || t.username == "" || t.password == "" || t.msgheader == "" {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.SendResult{}, classifyTransportError(err)
	}
	phone, ok := normalizeTRMobile(msg.phone)
	if !ok {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}
	body, err := t.encodeBody(msg.kind, phone, msg.message)
	if err != nil {
		return domain.SendResult{}, err
	}
	path := pathSend
	if msg.kind == endpointOTP {
		path = pathOTP
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, t.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return domain.SendResult{}, domain.ErrProviderRetryable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(t.username, t.password)

	start := time.Now()
	resp, err := t.client.Do(req)
	if err != nil {
		mapped := classifyTransportError(err)
		t.logCall(ctx, false, mapped, time.Since(start))
		return domain.SendResult{}, mapped
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes+1)
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		mapped := classifyTransportError(readErr)
		t.logCall(ctx, false, mapped, time.Since(start))
		return domain.SendResult{}, mapped
	}
	if len(raw) > maxResponseBytes {
		t.logCall(ctx, false, domain.ErrProviderRetryable, time.Since(start))
		return domain.SendResult{}, domain.ErrProviderRetryable
	}

	if resp.StatusCode == 429 || resp.StatusCode >= 500 || resp.StatusCode == 408 {
		mapped := classifyHTTPStatus(resp.StatusCode)
		t.logCall(ctx, false, mapped, time.Since(start))
		return domain.SendResult{}, mapped
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		t.logCall(ctx, false, domain.ErrProviderPermanent, time.Since(start))
		return domain.SendResult{}, domain.ErrProviderPermanent
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mapped := classifyHTTPStatus(resp.StatusCode)
		t.logCall(ctx, false, mapped, time.Since(start))
		return domain.SendResult{}, mapped
	}

	jobID, code, parseErr := parseAccepted(raw)
	if parseErr != nil {
		t.logCall(ctx, false, parseErr, time.Since(start))
		return domain.SendResult{}, parseErr
	}
	if classErr := classifyProviderCode(msg.kind, code); classErr != nil {
		t.logCall(ctx, false, classErr, time.Since(start))
		return domain.SendResult{}, classErr
	}
	t.logCall(ctx, true, nil, time.Since(start))
	return domain.SendResult{ProviderRef: jobID}, nil
}

func (t *Transport) encodeBody(kind endpointKind, phone, message string) ([]byte, error) {
	message = strings.TrimSpace(message)
	switch kind {
	case endpointOTP:
		if err := validateOTPBody(message); err != nil {
			return nil, err
		}
		return json.Marshal(otpRequest{
			MsgHeader: t.msgheader,
			Msg:       message,
			No:        phone,
		})
	case endpointSend:
		if err := validateTransactionalBody(message); err != nil {
			return nil, err
		}
		req := sendRequest{
			MsgHeader: t.msgheader,
			Messages: []sendMessage{{
				Msg: message,
				No:  phone,
			}},
		}
		if needsTREncoding(message) {
			enc := "TR"
			req.Encoding = &enc
		}
		return json.Marshal(req)
	default:
		return nil, domain.ErrInvalidDelivery
	}
}

type sendRequest struct {
	MsgHeader string        `json:"msgheader"`
	Encoding  *string       `json:"encoding,omitempty"`
	Messages  []sendMessage `json:"messages"`
}

type sendMessage struct {
	Msg string `json:"msg"`
	No  string `json:"no"`
}

type otpRequest struct {
	MsgHeader string `json:"msgheader"`
	Msg       string `json:"msg"`
	No        string `json:"no"`
}

type apiResponse struct {
	Code        json.RawMessage `json:"code"`
	Description string          `json:"description"`
	JobID       json.RawMessage `json:"jobid"`
	JobId       json.RawMessage `json:"jobId"`
}

func parseAccepted(raw []byte) (jobID, code string, err error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", "", domain.ErrProviderRetryable
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var parsed apiResponse
	if err := dec.Decode(&parsed); err != nil {
		return "", "", domain.ErrProviderRetryable
	}
	code = opaqueJobID(parsed.Code)
	if code == "" {
		return "", "", domain.ErrProviderRetryable
	}
	if code != "00" {
		return "", code, nil
	}
	jobID = opaqueJobID(parsed.JobID)
	if jobID == "" {
		jobID = opaqueJobID(parsed.JobId)
	}
	return jobID, code, nil
}

func opaqueJobID(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return strings.TrimSpace(n.String())
	}
	return strings.Trim(string(raw), `"`)
}

func (t *Transport) logCall(ctx context.Context, ok bool, err error, latency time.Duration) {
	attrs := []any{
		"provider", ProviderName,
		"channel_code", ChannelSMS,
		"ok", ok,
		"provider_latency_ms", latency.Milliseconds(),
	}
	if err != nil {
		class, _, _ := domain.ClassifyProviderError(err)
		attrs = append(attrs, "error_class", class)
	}
	observability.FromContext(ctx).Info("provider_call", attrs...)
}
