package ses

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	domain "backend/internal/notifications"
)

func classifySendError(err error) error {
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

	code := awsErrorCode(err)
	status := awsHTTPStatus(err)
	switch {
	case isPermanentCode(code):
		return domain.ErrProviderPermanent
	case isRetryableCode(code) || isRetryableStatus(status):
		if status == 0 && isTimeoutCode(code) {
			return domain.ErrProviderTimeout
		}
		return domain.ErrProviderRetryable
	case isRetryableStatus(status):
		return domain.ErrProviderRetryable
	case status >= 400 && status < 500:
		return domain.ErrProviderPermanent
	default:
		return domain.ErrProviderRetryable
	}
}

func awsErrorCode(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) && api != nil {
		return strings.TrimSpace(api.ErrorCode())
	}
	return ""
}

func awsHTTPStatus(err error) int {
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re != nil && re.Response != nil {
		return re.Response.StatusCode
	}
	return 0
}

func isPermanentCode(code string) bool {
	switch code {
	case "MessageRejected",
		"MailFromDomainNotVerifiedException",
		"MailFromDomainNotVerified",
		"AccountSuspendedException",
		"BadRequestException",
		"InvalidParameterValue",
		"InvalidParameterException",
		"NotFoundException",
		"ConfigurationSetDoesNotExistException",
		"ConfigurationSetDoesNotExist",
		"ValidationException":
		return true
	default:
		return false
	}
}

func isRetryableCode(code string) bool {
	switch code {
	case "TooManyRequestsException",
		"Throttling",
		"ThrottlingException",
		"LimitExceededException",
		"ServiceUnavailableException",
		"InternalFailure",
		"InternalServerError",
		"RequestTimeout",
		"RequestTimeoutException",
		"PriorRequestNotComplete",
		"ConcurrentModificationException",
		"AccountSendingPausedException",
		"ConfigurationSetSendingPausedException",
		"SendingPausedException":
		return true
	default:
		return false
	}
}

func isTimeoutCode(code string) bool {
	switch code {
	case "RequestTimeout", "RequestTimeoutException":
		return true
	default:
		return false
	}
}

func isRetryableStatus(status int) bool {
	return status == 408 || status == 429 || status >= 500
}
