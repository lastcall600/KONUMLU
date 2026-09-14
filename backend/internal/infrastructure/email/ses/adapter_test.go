package ses

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	domain "backend/internal/notifications"
	"backend/internal/notifications/contracts"
	"backend/internal/notifications/policy"
	"backend/internal/platform/config"
	"backend/internal/platform/observability"
)

type fakeSES struct {
	n     int
	last  *sesv2.SendEmailInput
	out   *sesv2.SendEmailOutput
	err   error
	delay time.Duration
}

func (f *fakeSES) SendEmail(ctx context.Context, params *sesv2.SendEmailInput, _ ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	f.n++
	f.last = params
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.out != nil {
		return f.out, nil
	}
	id := "010101-ses-message"
	return &sesv2.SendEmailOutput{MessageId: &id}, nil
}

type apiErr struct {
	code string
	msg  string
}

func (e apiErr) Error() string                 { return e.code + ": " + e.msg }
func (e apiErr) ErrorCode() string             { return e.code }
func (e apiErr) ErrorMessage() string          { return e.msg }
func (e apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func mustTransport(t *testing.T, api sesAPI) *Transport {
	t.Helper()
	tr, err := NewWithAPI(Config{Region: "eu-central-1", From: "noreply@example.test", Timeout: time.Second}, api)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestValidTransactionalSendAccepted(t *testing.T) {
	api := &fakeSES{}
	client, err := NewVerificationClient(mustTransport(t, api))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Send(context.Background(), domain.EmailSendRequest{
		Destination:        "owner@example.test",
		TemplateCode:       contracts.TemplateIdentityVerificationSignup,
		TemplateVersion:    1,
		Locale:             contracts.LocaleTR,
		VerificationSecret: "email-token-secret",
		IdempotencyKey:     "11111111-1111-1111-1111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProviderRef == "" {
		t.Fatal("accepted send should keep ephemeral SES MessageId on ProviderRef")
	}
	if api.n != 1 {
		t.Fatalf("calls=%d", api.n)
	}
	if api.last == nil || api.last.FromEmailAddress == nil || *api.last.FromEmailAddress != "noreply@example.test" {
		t.Fatalf("from = %v", api.last)
	}
	if api.last.Destination == nil || len(api.last.Destination.ToAddresses) != 1 {
		t.Fatal("expected one recipient")
	}
}

func TestTimeoutRetryable(t *testing.T) {
	api := &fakeSES{delay: 200 * time.Millisecond}
	tr, err := NewWithAPI(Config{Region: "eu-central-1", From: "noreply@example.test", Timeout: 20 * time.Millisecond}, api)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := NewVerificationClient(tr)
	_, err = c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderTimeout) {
		t.Fatalf("err=%v", err)
	}
	class, retryable, giveUp := domain.ClassifyProviderError(err)
	if class != domain.ErrorClassTimeout || !retryable || giveUp {
		t.Fatalf("class=%s retryable=%v giveUp=%v", class, retryable, giveUp)
	}
}

func TestThrottlingRetryableOnce(t *testing.T) {
	api := &fakeSES{err: apiErr{code: "TooManyRequestsException", msg: "throttled dest=owner@example.test token=secret-token"}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	_, err := c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
	if api.n != 1 {
		t.Fatalf("adapter must not retry internally calls=%d", api.n)
	}
}

func TestTransientProviderFailureRetryable(t *testing.T) {
	api := &fakeSES{err: &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 503}},
		Err:      apiErr{code: "ServiceUnavailableException", msg: "unavailable dest=owner@example.test"},
	}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	_, err := c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("err=%v", err)
	}
}

func TestInvalidDestinationPermanent(t *testing.T) {
	api := &fakeSES{err: apiErr{code: "MessageRejected", msg: "Email address is not verified: owner@example.test"}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	_, err := c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
	_, retryable, giveUp := domain.ClassifyProviderError(err)
	if retryable || !giveUp {
		t.Fatal("permanent must not retry")
	}
}

func TestSenderIdentityRejectionPermanent(t *testing.T) {
	api := &fakeSES{err: apiErr{code: "MailFromDomainNotVerifiedException", msg: "domain example.test"}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	_, err := c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
}

func TestMalformedProviderErrorPermanent(t *testing.T) {
	api := &fakeSES{err: apiErr{code: "BadRequestException", msg: "malformed"}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	_, err := c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
}

func TestUnconfiguredTransport(t *testing.T) {
	_, err := NewWithAPI(Config{Region: "eu-central-1", From: "noreply@example.test"}, nil)
	if !errors.Is(err, domain.ErrProviderUnconfigured) {
		t.Fatalf("err=%v", err)
	}
	var c *VerificationClient
	_, err = c.Send(context.Background(), validEmailReq())
	if !errors.Is(err, domain.ErrProviderUnconfigured) {
		t.Fatalf("nil client err=%v", err)
	}
}

func TestNoFakeSentOnProviderError(t *testing.T) {
	api := &fakeSES{err: apiErr{code: "TooManyRequestsException", msg: "slow"}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	res, err := c.Send(context.Background(), validEmailReq())
	if err == nil {
		t.Fatal("expected error")
	}
	if res.ProviderRef != "" {
		t.Fatalf("must not claim provider ref on failure: %+v", res)
	}
}

func TestLogsOmitRecipientBodyAndCredentials(t *testing.T) {
	var buf bytes.Buffer
	observability.ConfigureJSON(config.Config{Environment: config.EnvTest, LogLevel: "info"}, &buf)
	secret := "reset-token-SUPER-secret"
	dest := "synth.user@example.test"
	api := &fakeSES{err: apiErr{
		code: "TooManyRequestsException",
		msg:  "throttled dest=" + dest + " Authorization=AWS4 secret=" + secret + " body=<html>hi</html>",
	}}
	c, _ := NewVerificationClient(mustTransport(t, api))
	req := validEmailReq()
	req.Destination = dest
	req.VerificationSecret = secret
	_, err := c.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), dest) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "AWS4") {
		t.Fatalf("classified error leaked: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, dest) || strings.Contains(out, secret) || strings.Contains(out, "AWS4") || strings.Contains(out, "<html>") {
		t.Fatalf("log leaked: %s", out)
	}
	if !strings.Contains(out, `"provider":"ses"`) {
		t.Fatalf("missing provider metadata: %s", out)
	}
}

func TestChannelSenderUsesServerFromAndSkipsNonEmail(t *testing.T) {
	api := &fakeSES{}
	ch, err := NewChannelClient(mustTransport(t, api))
	if err != nil {
		t.Fatal(err)
	}
	_, err = ch.Send(context.Background(), domain.ChannelSendRequest{
		Channel:        policy.ChannelSMS,
		Destination:    "+15551234567",
		TemplateKey:    "identity.password.reset",
		Locale:         "tr",
		IdempotencyKey: "k",
	})
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("sms via email adapter err=%v", err)
	}
	if api.n != 0 {
		t.Fatal("non-email must not call SES")
	}
	_, err = ch.Send(context.Background(), domain.ChannelSendRequest{
		Channel:        policy.ChannelEmail,
		Destination:    "owner@example.test",
		TemplateKey:    "offer.received",
		Locale:         "tr",
		Variables:      map[string]string{"offer_id": "abc"},
		IdempotencyKey: "delivery-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if api.last == nil || api.last.FromEmailAddress == nil || *api.last.FromEmailAddress != "noreply@example.test" {
		t.Fatal("caller must not control From")
	}
}

func TestInvalidLocalDestinationDoesNotCallSES(t *testing.T) {
	api := &fakeSES{}
	c, _ := NewVerificationClient(mustTransport(t, api))
	req := validEmailReq()
	req.Destination = "not-an-email"
	_, err := c.Send(context.Background(), req)
	if !errors.Is(err, domain.ErrProviderPermanent) {
		t.Fatalf("err=%v", err)
	}
	if api.n != 0 {
		t.Fatal("malformed destination must not call SES")
	}
}

func TestConfigRejectsMissingFrom(t *testing.T) {
	if err := (Config{Region: "eu-central-1"}).Validate(); err == nil {
		t.Fatal("expected from required")
	}
}

func validEmailReq() domain.EmailSendRequest {
	return domain.EmailSendRequest{
		Destination:        "owner@example.test",
		TemplateCode:       contracts.TemplateIdentityPasswordReset,
		TemplateVersion:    1,
		Locale:             contracts.LocaleEN,
		VerificationSecret: "email-token-secret",
		IdempotencyKey:     "22222222-2222-2222-2222-222222222222",
	}
}
