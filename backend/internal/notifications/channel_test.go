package notifications

import (
	"strings"
	"testing"

	"backend/internal/notifications/policy"
)

func TestChannelSendRequestDoesNotPrintDestination(t *testing.T) {
	req := ChannelSendRequest{Channel: policy.ChannelEmail, Destination: "owner@example.test", TemplateKey: "marketing.campaign", IdempotencyKey: "k"}
	if strings.Contains(req.String(), "owner@") || strings.Contains(req.GoString(), "owner@") {
		t.Fatal("destination leaked")
	}
}

func TestClassifyProviderError(t *testing.T) {
	class, retry, _ := ClassifyProviderError(ErrProviderPermanent)
	if class != ErrorClassPermanent || retry {
		t.Fatalf("permanent class=%s retry=%v", class, retry)
	}
	class, retry, _ = ClassifyProviderError(ErrProviderTimeout)
	if class != ErrorClassTimeout || !retry {
		t.Fatalf("timeout class=%s retry=%v", class, retry)
	}
	class, retry, _ = ClassifyProviderError(ErrProviderUnconfigured)
	if class != ErrorClassUnconfigured || !retry {
		t.Fatalf("unconfigured class=%s retry=%v", class, retry)
	}
}

func TestRenderPlainEscapes(t *testing.T) {
	got := RenderPlain("offer.received", "en", map[string]string{"offer_id": "<b>x</b>"})
	if strings.Contains(got, "<b>") {
		t.Fatalf("unescaped html: %s", got)
	}
	if !strings.Contains(got, "offer.received|en") {
		t.Fatalf("render = %s", got)
	}
}
