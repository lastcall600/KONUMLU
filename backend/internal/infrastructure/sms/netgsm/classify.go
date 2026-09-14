package netgsm

import (
	"context"
	"errors"
	"net"
	"strings"

	domain "backend/internal/notifications"
)

type endpointKind string

const (
	endpointSend endpointKind = "send"
	endpointOTP  endpointKind = "otp"
)

func classifyHTTPStatus(status int) error {
	if status == 0 {
		return domain.ErrProviderRetryable
	}
	if status == 408 || status == 429 || status >= 500 {
		return domain.ErrProviderRetryable
	}
	if status == 401 || status == 403 {
		return domain.ErrProviderPermanent
	}
	if status >= 400 && status < 500 {
		return domain.ErrProviderPermanent
	}
	return domain.ErrProviderRetryable
}

func classifyProviderCode(kind endpointKind, code string) error {
	code = strings.TrimSpace(code)
	if code == "00" {
		return nil
	}
	switch kind {
	case endpointSend:
		return classifySendCode(code)
	case endpointOTP:
		return classifyOTPCode(code)
	default:
		return genericSystemFallback()
	}
}

// classifySendCode maps REST v2 POST /sms/rest/v2/send codes only.
// Documented contract: 20/30/40/50/51/70 permanent; 80/85 retryable.
func classifySendCode(code string) error {
	switch code {
	case "20", "30", "40", "50", "51", "70":
		return domain.ErrProviderPermanent
	case "80", "85":
		return domain.ErrProviderRetryable
	default:
		return genericSystemFallback()
	}
}

// classifyOTPCode maps REST v2 POST /sms/rest/v2/otp codes only.
// Documented contract: 20/30/40/41/50/51/52/60/70 permanent; 100 retryable.
func classifyOTPCode(code string) error {
	switch code {
	case "20", "30", "40", "41", "50", "51", "52", "60", "70":
		return domain.ErrProviderPermanent
	case "100":
		return domain.ErrProviderRetryable
	default:
		return genericSystemFallback()
	}
}

// genericSystemFallback is not part of either endpoint's documented code list.
// It covers ambiguous/legacy values such as 101 and 5000 without assigning
// endpoint-specific meaning. Dispatcher still owns retry.
func genericSystemFallback() error {
	return domain.ErrProviderRetryable
}

func classifyTransportError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.ErrProviderTimeout
	}
	if errors.Is(err, domain.ErrProviderPermanent) ||
		errors.Is(err, domain.ErrProviderRetryable) ||
		errors.Is(err, domain.ErrProviderTimeout) ||
		errors.Is(err, domain.ErrProviderUnconfigured) ||
		errors.Is(err, domain.ErrInvalidDelivery) ||
		errors.Is(err, domain.ErrUnavailable) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil && netErr.Timeout() {
		return domain.ErrProviderTimeout
	}
	return domain.ErrProviderRetryable
}
