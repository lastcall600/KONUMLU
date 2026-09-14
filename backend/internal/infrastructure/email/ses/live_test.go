package ses

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	domain "backend/internal/notifications"
	"backend/internal/notifications/contracts"
)

func TestLiveSESSendIfConfigured(t *testing.T) {
	dest := strings.TrimSpace(os.Getenv("KONUMLU_SES_LIVE_TO"))
	if dest == "" {
		t.Skip("LIVE_SES_TEST_PENDING")
	}
	cfg := Config{
		Region:  strings.TrimSpace(os.Getenv("EMAIL_SES_REGION")),
		From:    strings.TrimSpace(os.Getenv("EMAIL_SES_FROM")),
		Timeout: 10 * time.Second,
	}
	tr, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("ses transport: %v", err)
	}
	c, err := NewVerificationClient(tr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), domain.EmailSendRequest{
		Destination:        dest,
		TemplateCode:       contracts.TemplateIdentityVerificationSignup,
		TemplateVersion:    1,
		Locale:             contracts.LocaleEN,
		VerificationSecret: "live-test-not-a-login-secret",
		IdempotencyKey:     "live-ses-konumlu-test",
	})
	if err != nil {
		t.Fatalf("live send class=%v", err)
	}
}
