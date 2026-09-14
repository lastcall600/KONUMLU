package notifications

import (
	"testing"

	"backend/internal/notifications/policy"
)

func TestPushFanoutAggregation(t *testing.T) {
	cases := []struct {
		name  string
		in    pushFanout
		state policy.DeliveryState
	}{
		{name: "one accepted", in: pushFanout{configured: 1, accepted: 1}, state: policy.DeliveryAccepted},
		{name: "partial success hides retryable", in: pushFanout{configured: 2, accepted: 1, retryable: 1, retryClass: ErrorClassRetryable}, state: policy.DeliveryAccepted},
		{name: "partial success with invalid", in: pushFanout{configured: 2, accepted: 1, invalid: 1}, state: policy.DeliveryAccepted},
		{name: "retryable not hidden", in: pushFanout{configured: 2, retryable: 1, invalid: 1, retryClass: ErrorClassTimeout}, state: policy.DeliveryRetryableFailed},
		{name: "all invalid", in: pushFanout{configured: 2, invalid: 2}, state: policy.DeliverySuppressed},
		{name: "unconfigured only", in: pushFanout{unconfigured: 2}, state: policy.DeliveryPending},
		{name: "permanent", in: pushFanout{configured: 1, permanent: 1, permClass: ErrorClassPermanent}, state: policy.DeliveryPermanentlyFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, _, _ := tc.in.decide()
			if state != tc.state {
				t.Fatalf("state=%s want=%s", state, tc.state)
			}
		})
	}
}
