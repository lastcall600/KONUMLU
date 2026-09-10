package eids

import (
	"errors"
	"testing"
	"time"
)

func TestPropertyAndVehicleAreIndependent(t *testing.T) {
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	listing := mustEIDSID(t)
	prop, err := NewPending(listing, TypeProperty, now)
	if err != nil {
		t.Fatal(err)
	}
	veh, err := NewPending(listing, TypeVehicle, now)
	if err != nil {
		t.Fatal(err)
	}
	if prop.VerificationType == veh.VerificationType {
		t.Fatal("types must differ")
	}
	verified, err := prop.ApplyOutcome(ProviderOutcome{Outcome: OutcomeVerified}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status.PublishEligible() && veh.Status.PublishEligible() {
		t.Fatal("vehicle must not inherit property pass")
	}
}

func TestTransitionsAndTerminalRetry(t *testing.T) {
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	v, err := NewPending(mustEIDSID(t), TypeProperty, now)
	if err != nil {
		t.Fatal(err)
	}
	unavail, err := v.ApplyOutcome(ProviderOutcome{Outcome: OutcomeUnavailable}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if unavail.FailureCode != FailureProviderUnavailable {
		t.Fatalf("failure = %s", unavail.FailureCode)
	}
	retried, err := unavail.ApplyOutcome(ProviderOutcome{Outcome: OutcomeVerified}, now.Add(2*time.Second))
	if err != nil || retried.Status != StatusVerified {
		t.Fatalf("retry from unavailable = %+v err=%v", retried, err)
	}
	if _, err := retried.ApplyOutcome(ProviderOutcome{Outcome: OutcomeFailed}, now.Add(3*time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("verified is terminal: %v", err)
	}
	failedBase, err := NewPending(mustEIDSID(t), TypeVehicle, now)
	if err != nil {
		t.Fatal(err)
	}
	failed, err := failedBase.ApplyOutcome(ProviderOutcome{Outcome: OutcomeFailed}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failed.ApplyOutcome(ProviderOutcome{Outcome: OutcomeVerified}, now.Add(2*time.Second)); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("failed is terminal: %v", err)
	}
}

func TestExpiredVerifiedIsNotEligible(t *testing.T) {
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	v, err := NewPending(mustEIDSID(t), TypeProperty, now)
	if err != nil {
		t.Fatal(err)
	}
	exp := now.Add(time.Minute)
	verified, err := v.ApplyOutcome(ProviderOutcome{Outcome: OutcomeVerified, ExpiresAt: &exp}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !verified.Status.PublishEligible() {
		t.Fatal("verified should be eligible before expiry")
	}
	expired, err := verified.Effective(now.Add(2 * time.Minute))
	if err != nil || expired.Status != StatusExpired {
		t.Fatalf("expired = %+v err=%v", expired, err)
	}
	if expired.Status.PublishEligible() {
		t.Fatal("expired must not be eligible")
	}
}

func TestPersonIdentityTypeRejected(t *testing.T) {
	if _, err := ParseType("identity"); !errors.Is(err, errInvalidType) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewPending(mustEIDSID(t), VerificationType("generic"), time.Now().UTC()); !errors.Is(err, errInvalidType) {
		t.Fatalf("err = %v", err)
	}
}

func mustEIDSID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
