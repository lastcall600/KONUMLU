package notifications

import "backend/internal/notifications/policy"

// pushFanout is the V1 aggregation of independent endpoint sends onto one
// channel_deliveries row. There are no per-endpoint delivery rows.
//
// Rule:
//   - every eligible endpoint is attempted once (no adapter retry)
//   - channel accepted if at least one configured endpoint is provider-accepted
//   - permanently invalid endpoints are revoked and do not by themselves retry
//   - if nothing was accepted, retryable failures are not hidden
//   - if nothing was accepted and every remaining outcome is unconfigured, park
//   - if nothing was accepted and all configured endpoints were revoked, suppress
//
// Mixed accept + retryable is accepted and is not retried. Devices that failed
// retryably on that attempt are not guaranteed a later send for this intent
// (no per-endpoint cursor). Full-channel retry (all retryable) is at-least-once
// and may duplicate to endpoints that later succeed.
type pushFanout struct {
	configured   int
	accepted     int
	unconfigured int
	invalid      int
	retryable    int
	permanent    int
	retryClass   string
	permClass    string
	lastRef      string
}

func (a *pushFanout) noteRetryable(class string) {
	a.retryable++
	if a.retryClass == "" {
		a.retryClass = class
	}
}

func (a *pushFanout) notePermanent(class string) {
	a.permanent++
	if a.permClass == "" {
		a.permClass = class
	}
}

func (a pushFanout) decide() (policy.DeliveryState, string, policy.SuppressionReason) {
	if a.accepted > 0 {
		return policy.DeliveryAccepted, "", ""
	}
	if a.configured == 0 {
		return policy.DeliveryPending, ErrorClassUnconfigured, ""
	}
	if a.retryable > 0 {
		class := a.retryClass
		if class == "" {
			class = ErrorClassRetryable
		}
		return policy.DeliveryRetryableFailed, class, ""
	}
	if a.permanent > 0 {
		class := a.permClass
		if class == "" {
			class = ErrorClassPermanent
		}
		return policy.DeliveryPermanentlyFailed, class, ""
	}
	return policy.DeliverySuppressed, "", policy.SuppressNoDestination
}
