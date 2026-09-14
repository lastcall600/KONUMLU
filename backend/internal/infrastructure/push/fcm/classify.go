package fcm

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
	switch {
	case status == http.StatusOK:
		return nil
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return domain.ErrProviderRetryable
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return domain.ErrProviderPermanent
	}

	code := fcmErrorCode(body)
	switch code {
	case "UNREGISTERED", "INVALID_ARGUMENT":
		if code == "INVALID_ARGUMENT" && !tokenInvalid(body) {
			return domain.ErrProviderPermanent
		}
		return domain.ErrProviderEndpointInvalid
	case "SENDER_ID_MISMATCH", "THIRD_PARTY_AUTH_ERROR":
		return domain.ErrProviderPermanent
	case "UNAVAILABLE", "INTERNAL", "QUOTA_EXCEEDED":
		return domain.ErrProviderRetryable
	}
	if status == http.StatusNotFound {
		return domain.ErrProviderEndpointInvalid
	}
	if status >= 400 && status < 500 {
		return domain.ErrProviderPermanent
	}
	return domain.ErrProviderRetryable
}

func fcmErrorCode(body []byte) string {
	var parsed struct {
		Error struct {
			Status  string `json:"status"`
			Details []struct {
				Type      string `json:"@type"`
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ""
	}
	for _, d := range parsed.Error.Details {
		if strings.Contains(d.Type, "FcmError") && d.ErrorCode != "" {
			return strings.ToUpper(strings.TrimSpace(d.ErrorCode))
		}
	}
	return strings.ToUpper(strings.TrimSpace(parsed.Error.Status))
}

func tokenInvalid(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "registration token") ||
		strings.Contains(s, "not a valid fcm") ||
		strings.Contains(s, "requested entity was not found")
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
