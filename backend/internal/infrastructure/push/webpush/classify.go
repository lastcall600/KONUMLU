package webpush

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	domain "backend/internal/notifications"
)

func classifyHTTPStatus(status int) error {
	switch {
	case status == http.StatusCreated || status == http.StatusOK:
		return nil
	case status == http.StatusNotFound || status == http.StatusGone:
		return domain.ErrProviderEndpointInvalid
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return domain.ErrProviderRetryable
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return domain.ErrProviderPermanent
	case status >= 400 && status < 500:
		return domain.ErrProviderPermanent
	default:
		return domain.ErrProviderRetryable
	}
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
		errors.Is(err, domain.ErrProviderEndpointInvalid) ||
		errors.Is(err, domain.ErrInvalidDelivery) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil && netErr.Timeout() {
		return domain.ErrProviderTimeout
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "vapid") || strings.Contains(msg, "malformed") || strings.Contains(msg, "invalid") {
		return domain.ErrProviderPermanent
	}
	return domain.ErrProviderRetryable
}

func drainBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, 64<<10)
	_ = body.Close()
}
