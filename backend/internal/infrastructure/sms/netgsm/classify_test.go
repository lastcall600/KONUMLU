package netgsm

import (
	"errors"
	"testing"

	domain "backend/internal/notifications"
)

func TestClassifySendDocumentedCodes(t *testing.T) {
	permanent := []string{"20", "30", "40", "50", "51", "70"}
	for _, code := range permanent {
		if err := classifyProviderCode(endpointSend, code); !errors.Is(err, domain.ErrProviderPermanent) {
			t.Fatalf("send %s: %v", code, err)
		}
	}
	retryable := []string{"80", "85"}
	for _, code := range retryable {
		if err := classifyProviderCode(endpointSend, code); !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("send %s: %v", code, err)
		}
	}
}

func TestClassifyOTPDocumentedCodes(t *testing.T) {
	permanent := []string{"20", "30", "40", "41", "50", "51", "52", "60", "70"}
	for _, code := range permanent {
		if err := classifyProviderCode(endpointOTP, code); !errors.Is(err, domain.ErrProviderPermanent) {
			t.Fatalf("otp %s: %v", code, err)
		}
	}
	if err := classifyProviderCode(endpointOTP, "100"); !errors.Is(err, domain.ErrProviderRetryable) {
		t.Fatalf("otp 100: %v", err)
	}
}

func TestClassifyDoesNotInventCrossEndpointMeanings(t *testing.T) {
	// Send does not document 41/52/60 as OTP header/destination/package codes.
	for _, code := range []string{"41", "52", "60"} {
		if err := classifyProviderCode(endpointSend, code); !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("send undocumented %s must be generic fallback, got %v", code, err)
		}
	}
	// OTP does not document 80/85 send-limit / duplicate-rate semantics.
	for _, code := range []string{"80", "85"} {
		if err := classifyProviderCode(endpointOTP, code); !errors.Is(err, domain.ErrProviderRetryable) {
			t.Fatalf("otp undocumented %s must be generic fallback, got %v", code, err)
		}
	}
}

func TestClassifyGenericSystemFallback(t *testing.T) {
	for _, kind := range []endpointKind{endpointSend, endpointOTP} {
		for _, code := range []string{"101", "5000"} {
			if err := classifyProviderCode(kind, code); !errors.Is(err, domain.ErrProviderRetryable) {
				t.Fatalf("%s %s fallback: %v", kind, code, err)
			}
		}
	}
}
