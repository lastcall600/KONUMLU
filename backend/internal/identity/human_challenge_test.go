package identity

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeHumanChallengeAcceptsBoundAction(t *testing.T) {
	f := FakeHumanChallenge{}
	got, err := f.Verify(context.Background(), HumanChallengeInput{
		Token:  "ok:password_login",
		Action: HumanChallengeAction(AuthOpPasswordLogin),
	})
	if err != nil || !got.OK {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestFakeHumanChallengeRejectsWrongActionAndToken(t *testing.T) {
	f := FakeHumanChallenge{}
	got, err := f.Verify(context.Background(), HumanChallengeInput{
		Token:  "ok:password_login",
		Action: HumanChallengeAction(AuthOpSignupComplete),
	})
	if err != nil || got.OK {
		t.Fatalf("wrong action must fail: %+v %v", got, err)
	}
	got, err = f.Verify(context.Background(), HumanChallengeInput{
		Token:  "nope",
		Action: HumanChallengeAction(AuthOpPasswordLogin),
	})
	if err != nil || got.OK {
		t.Fatal("invalid token must fail")
	}
}

func TestFakeHumanChallengeHostname(t *testing.T) {
	f := FakeHumanChallenge{ExpectedHostname: "app.example.test"}
	got, err := f.Verify(context.Background(), HumanChallengeInput{
		Token:    "ok:password_login",
		Action:   HumanChallengeAction(AuthOpPasswordLogin),
		Hostname: "evil.example",
	})
	if err != nil || got.OK {
		t.Fatal("wrong host must fail")
	}
	got, err = f.Verify(context.Background(), HumanChallengeInput{
		Token:    "ok:password_login",
		Action:   HumanChallengeAction(AuthOpPasswordLogin),
		Hostname: "app.example.test",
	})
	if err != nil || !got.OK {
		t.Fatalf("matching host: %+v %v", got, err)
	}
}

func TestUnconfiguredHumanChallengeNeverSucceeds(t *testing.T) {
	got, err := UnconfiguredHumanChallenge{}.Verify(context.Background(), HumanChallengeInput{
		Token:  "ok:password_login",
		Action: HumanChallengeAction(AuthOpPasswordLogin),
	})
	if err == nil || got.OK || !errors.Is(err, errUnavailable) {
		t.Fatalf("unconfigured must fail closed: %+v %v", got, err)
	}
}

func TestChallengeRequiredUsesFakeThenReplay(t *testing.T) {
	ctx := context.Background()
	inc := &memCounter{}
	policy := testAbusePolicy(t)
	ch := HumanChallengePolicy{
		Provider:  HumanChallengeProviderFake,
		Required:  map[AuthOperation]struct{}{AuthOpPasswordLogin: {}},
		ReplayTTL: time.Minute,
	}
	eng, err := NewAbuseEngine(inc, policy, ch, FakeHumanChallenge{})
	if err != nil {
		t.Fatal(err)
	}
	sub := AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1"}
	out := eng.Evaluate(ctx, sub, PhaseChallenge)
	if out.Allow() || out.Decision != RiskChallenge || out.Reason != ReasonChallengeRequired {
		t.Fatalf("missing token: %+v", out)
	}
	sub.ChallengeToken = "ok:password_login"
	out = eng.Evaluate(ctx, sub, PhaseChallenge)
	if !out.Allow() {
		t.Fatalf("valid fake: %+v", out)
	}
	out = eng.Evaluate(ctx, sub, PhaseChallenge)
	if out.Allow() || out.Reason != ReasonChallengeFailed {
		t.Fatalf("replay: %+v", out)
	}
}

func TestChallengeProviderErrorFailsClosed(t *testing.T) {
	ch := HumanChallengePolicy{
		Provider:  HumanChallengeProviderFake,
		Required:  map[AuthOperation]struct{}{AuthOpSignupComplete: {}},
		ReplayTTL: time.Minute,
	}
	eng, err := NewAbuseEngine(&memCounter{}, testAbusePolicy(t), ch, FakeHumanChallenge{Err: context.DeadlineExceeded})
	if err != nil {
		t.Fatal(err)
	}
	out := eng.Evaluate(context.Background(), AbuseSubject{
		Operation:      AuthOpSignupComplete,
		IP:             "10.0.0.2",
		Proof:          "proof",
		ChallengeToken: "ok:signup_complete",
	}, PhaseChallenge)
	if out.Allow() || out.Reason != ReasonProviderUnavailable {
		t.Fatalf("timeout: %+v", out)
	}
}

func TestChallengeNotRequiredDoesNotCallProvider(t *testing.T) {
	eng, err := NewAbuseEngine(&memCounter{}, testAbusePolicy(t), HumanChallengePolicy{}, panicChallenge{})
	if err != nil {
		t.Fatal(err)
	}
	out := eng.Evaluate(context.Background(), AbuseSubject{Operation: AuthOpPasswordLogin, IP: "1.1.1.1"}, PhaseChallenge)
	if !out.Allow() {
		t.Fatalf("out = %+v", out)
	}
}

type panicChallenge struct{}

func (panicChallenge) Name() string { return "panic" }
func (panicChallenge) Verify(context.Context, HumanChallengeInput) (HumanChallengeResult, error) {
	panic("provider must not be called")
}
