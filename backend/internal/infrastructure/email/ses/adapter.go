// Package ses is the Amazon SES transactional email transport.
//
// It is not notification policy, Identity, SMS, Push, Staff IAM, Trust,
// Turnstile, or the Türkiye Compliance Gateway. SendEmail acceptance means
// SES accepted/queued the message. It does not prove end-user delivery.
// SES has no native exactly-once guarantee on this path; dispatcher and
// outbox semantics remain at-least-once on ambiguous failures.
package ses

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	domain "backend/internal/notifications"
	"backend/internal/platform/observability"
)

type sesAPI interface {
	SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// Transport sends transactional email through SES API v2 SendEmail.
// It performs one request per call. SDK retries are disabled; bounded
// retry/backoff is owned by the Notifications dispatcher / outbox.
type Transport struct {
	from    string
	region  string
	timeout time.Duration
	api     sesAPI
}

// New loads the AWS default credential provider chain (environment,
// shared config, task/instance role, workload identity). Access keys are
// never taken from KONUMLU application config and must never be logged.
func New(ctx context.Context, cfg Config) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.normalized()
	if ctx == nil {
		ctx = context.Background()
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithRetryer(func() aws.Retryer { return aws.NopRetryer{} }),
	)
	if err != nil {
		return nil, domain.ErrProviderUnconfigured
	}
	client := sesv2.NewFromConfig(awsCfg, func(o *sesv2.Options) {
		o.Region = cfg.Region
		o.RetryMaxAttempts = 1
		o.Retryer = aws.NopRetryer{}
		o.HTTPClient = &http.Client{Timeout: cfg.Timeout}
	})
	return NewWithAPI(cfg, client)
}

// NewWithAPI constructs a transport against an injected SES client (tests).
func NewWithAPI(cfg Config, api sesAPI) (*Transport, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if api == nil {
		return nil, domain.ErrProviderUnconfigured
	}
	cfg = cfg.normalized()
	return &Transport{
		from:    strings.TrimSpace(cfg.From),
		region:  cfg.Region,
		timeout: cfg.Timeout,
		api:     api,
	}, nil
}

type outbound struct {
	Recipient      string
	Subject        string
	Text           string
	HTML           string
	IdempotencyKey string
}

func (t *Transport) deliver(ctx context.Context, msg outbound) (domain.SendResult, error) {
	if t == nil || t.api == nil || t.from == "" {
		return domain.SendResult{}, domain.ErrProviderUnconfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return domain.SendResult{}, classifySendError(err)
	}
	if !validDestination(msg.Recipient) || strings.TrimSpace(msg.Subject) == "" || strings.TrimSpace(msg.Text) == "" {
		return domain.SendResult{}, domain.ErrProviderPermanent
	}
	if strings.TrimSpace(msg.IdempotencyKey) == "" {
		return domain.SendResult{}, domain.ErrInvalidDelivery
	}

	in := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(t.from),
		Destination: &types.Destination{
			ToAddresses: []string{strings.TrimSpace(msg.Recipient)},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{Data: aws.String(msg.Subject), Charset: aws.String("UTF-8")},
				Body: &types.Body{
					Text: &types.Content{Data: aws.String(msg.Text), Charset: aws.String("UTF-8")},
				},
			},
		},
		EmailTags: []types.MessageTag{{
			Name:  aws.String("konumlu_delivery_id"),
			Value: aws.String(sanitizeTag(msg.IdempotencyKey)),
		}},
	}
	if strings.TrimSpace(msg.HTML) != "" {
		in.Content.Simple.Body.Html = &types.Content{Data: aws.String(msg.HTML), Charset: aws.String("UTF-8")}
	}

	callCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	start := time.Now()
	out, err := t.api.SendEmail(callCtx, in)
	class := ""
	if err != nil {
		mapped := classifySendError(err)
		class, _, _ = domain.ClassifyProviderError(mapped)
		observability.FromContext(ctx).Info("provider_call",
			"provider", ProviderName,
			"channel_code", ChannelEmail,
			"ok", false,
			"error_class", class,
			"provider_latency_ms", time.Since(start).Milliseconds(),
		)
		return domain.SendResult{}, mapped
	}
	observability.FromContext(ctx).Info("provider_call",
		"provider", ProviderName,
		"channel_code", ChannelEmail,
		"ok", true,
		"provider_latency_ms", time.Since(start).Milliseconds(),
	)
	ref := ""
	if out != nil && out.MessageId != nil {
		ref = strings.TrimSpace(*out.MessageId)
	}
	return domain.SendResult{ProviderRef: ref}, nil
}

func sanitizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "missing"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if utf8.RuneCountInString(b.String()) >= 256 {
			break
		}
	}
	out := b.String()
	if out == "" {
		return "missing"
	}
	return out
}
