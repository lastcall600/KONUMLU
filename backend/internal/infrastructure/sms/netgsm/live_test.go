package netgsm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	domain "backend/internal/notifications"
	"backend/internal/notifications/contracts"
)

func TestLiveNetgsmSendIfConfigured(t *testing.T) {
	dest := strings.TrimSpace(os.Getenv("KONUMLU_NETGSM_LIVE_TO"))
	if dest == "" {
		t.Skip("LIVE_NETGSM_TEST_PENDING")
	}
	cfg := Config{
		Username:  strings.TrimSpace(os.Getenv("NETGSM_USERNAME")),
		Password:  strings.TrimSpace(os.Getenv("NETGSM_PASSWORD")),
		MsgHeader: strings.TrimSpace(os.Getenv("NETGSM_MSGHEADER")),
		Timeout:   10 * time.Second,
	}
	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("netgsm transport: %v", err)
	}
	c, err := NewOTPClient(tr)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Send(context.Background(), domain.SMSSendRequest{
		Destination:        dest,
		TemplateCode:       contracts.TemplateIdentityVerificationSignup,
		TemplateVersion:    1,
		Locale:             contracts.LocaleEN,
		VerificationSecret: "000000",
		IdempotencyKey:     "live-netgsm-konumlu-test",
	})
	if err != nil {
		t.Fatalf("live send class=%v", err)
	}
}
