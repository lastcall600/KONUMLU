package apns

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	domain "backend/internal/notifications"
)

func classifyHTTP(status int, body []byte) error {
	reason := apnsReason(body)
	switch reason {
	case "BadDeviceToken", "Unregistered", "DeviceTokenNotForTopic", "ExpiredProviderToken":
		if reason == "ExpiredProviderToken" {
			return domain.ErrProviderRetryable
		}
		return domain.ErrProviderEndpointInvalid
	case "BadTopic", "TopicDisallowed", "InvalidProviderToken", "MissingProviderToken",
		"Forbidden", "MissingTopic", "BadMessageId", "PayloadEmpty", "PayloadTooLarge",
		"BadPriority", "BadExpirationDate", "BadCollapseId", "DuplicateHeaders", "IdleTimeout":
		if reason == "IdleTimeout" {
			return domain.ErrProviderRetryable
		}
		return domain.ErrProviderPermanent
	case "TooManyRequests", "TooManyProviderTokenUpdates", "ServiceUnavailable", "Shutdown", "InternalServerError":
		return domain.ErrProviderRetryable
	}

	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusGone:
		return domain.ErrProviderEndpointInvalid
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return domain.ErrProviderRetryable
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return domain.ErrProviderPermanent
	case status == http.StatusBadRequest:
		if reason == "BadDeviceToken" {
			return domain.ErrProviderEndpointInvalid
		}
		return domain.ErrProviderPermanent
	case status >= 400 && status < 500:
		return domain.ErrProviderPermanent
	default:
		return domain.ErrProviderRetryable
	}
}

func apnsReason(body []byte) string {
	var parsed struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Reason)
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
		errors.Is(err, domain.ErrProviderEndpointInvalid) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr != nil && netErr.Timeout() {
		return domain.ErrProviderTimeout
	}
	return domain.ErrProviderRetryable
}

func drainLimited(r io.Reader) []byte {
	if r == nil {
		return nil
	}
	b, _ := io.ReadAll(io.LimitReader(r, 64<<10))
	return b
}
