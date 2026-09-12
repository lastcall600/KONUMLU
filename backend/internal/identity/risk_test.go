package identity

import (
	"reflect"
	"testing"
)

func TestRiskDecisionOnlyAllowsKnownValues(t *testing.T) {
	for _, d := range []RiskDecision{RiskAllow, RiskChallenge, RiskStepUp, RiskRestrict, RiskReview} {
		if !d.valid() {
			t.Fatalf("rejected %q", d)
		}
	}
	if RiskDecision("allow_with_score").valid() || RiskDecision("ban").valid() || RiskDecision("new").valid() {
		t.Fatal("unknown decisions must be rejected")
	}
}

func TestRiskOutcomeHasNoNumericScore(t *testing.T) {
	rt := reflect.TypeOf(RiskOutcome{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		switch f.Type.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
			reflect.Float32, reflect.Float64:
			if f.Name == "RetryAfter" {
				continue
			}
			t.Fatalf("numeric field %s would look like a score", f.Name)
		}
		if f.Name == "Score" || f.Name == "Trust" || f.Name == "Level" {
			t.Fatalf("forbidden field %s", f.Name)
		}
	}
}

func TestRateLimitBreachCannotAllow(t *testing.T) {
	out := RiskOutcome{Decision: RiskRestrict, Reason: ReasonVelocityIP}
	if out.Allow() {
		t.Fatal("restrict must not allow")
	}
	if out.Decision == RiskAllow {
		t.Fatal("velocity cannot be allow")
	}
}

func TestChallengeRequiredCannotAllow(t *testing.T) {
	out := RiskOutcome{Decision: RiskChallenge, Reason: ReasonChallengeRequired}
	if out.Allow() {
		t.Fatal("challenge must not allow")
	}
}

func TestReasonCodesAreControlled(t *testing.T) {
	if ReasonVelocityIP.valid() == false || !ReasonStorageUnavailable.valid() {
		t.Fatal("expected reasons")
	}
	if RiskReason("ml_score").valid() || RiskReason("established").valid() {
		t.Fatal("speculative reasons")
	}
}
