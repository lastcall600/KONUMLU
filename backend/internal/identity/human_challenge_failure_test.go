package identity

import (
	"context"
	"testing"
	"time"
)

func TestHumanChallengeProviderFailureMatrixFailClosed(t *testing.T) {
	policy := HumanChallengePolicy{
		Provider:  HumanChallengeProviderFake,
		Required:  map[AuthOperation]struct{}{AuthOpPasswordLogin: {}},
		Hostname:  "app.example.test",
		ReplayTTL: time.Minute,
	}
	cases := []struct {
		name string
		v    HumanChallenge
		in   AbuseSubject
		want RiskReason
	}{
		{
			name: "timeout",
			v:    FakeHumanChallenge{Err: context.DeadlineExceeded},
			in:   AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", ChallengeToken: "ok:password_login", Hostname: "app.example.test"},
			want: ReasonProviderUnavailable,
		},
		{
			name: "unavailable",
			v:    UnconfiguredHumanChallenge{},
			in:   AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", ChallengeToken: "ok:password_login", Hostname: "app.example.test"},
			want: ReasonProviderUnavailable,
		},
		{
			name: "invalid_token",
			v:    FakeHumanChallenge{ExpectedHostname: "app.example.test"},
			in:   AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", ChallengeToken: "bad", Hostname: "app.example.test"},
			want: ReasonChallengeFailed,
		},
		{
			name: "wrong_action",
			v:    FakeHumanChallenge{ExpectedHostname: "app.example.test"},
			in:   AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", ChallengeToken: "ok:signup_complete", Hostname: "app.example.test"},
			want: ReasonChallengeFailed,
		},
		{
			name: "wrong_hostname",
			v:    FakeHumanChallenge{ExpectedHostname: "app.example.test"},
			in:   AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", ChallengeToken: "ok:password_login", Hostname: "evil.example"},
			want: ReasonChallengeFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, err := NewAbuseEngine(&memCounter{}, testAbusePolicy(t), policy, tc.v)
			if err != nil {
				t.Fatal(err)
			}
			out := eng.Evaluate(context.Background(), tc.in, PhaseChallenge)
			if out.Allow() {
				t.Fatalf("must not allow: %+v", out)
			}
			if out.Reason != tc.want {
				t.Fatalf("reason = %s want %s (%+v)", out.Reason, tc.want, out)
			}
		})
	}

	eng, err := NewAbuseEngine(&memCounter{}, testAbusePolicy(t), policy, FakeHumanChallenge{ExpectedHostname: "app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	ok := AbuseSubject{Operation: AuthOpPasswordLogin, IP: "10.0.0.1", ChallengeToken: "ok:password_login", Hostname: "app.example.test"}
	out := eng.Evaluate(context.Background(), ok, PhaseChallenge)
	if !out.Allow() {
		t.Fatalf("valid token: %+v", out)
	}
	out = eng.Evaluate(context.Background(), ok, PhaseChallenge)
	if out.Allow() || out.Reason != ReasonChallengeFailed {
		t.Fatalf("replay must fail closed: %+v", out)
	}
}

func TestUnconfiguredRequiredChallengeNeverAllows(t *testing.T) {
	ch := HumanChallengePolicy{
		Provider:  HumanChallengeProviderUnconfigured,
		Required:  map[AuthOperation]struct{}{AuthOpSignupComplete: {}},
		ReplayTTL: time.Minute,
	}
	eng, err := NewAbuseEngine(&memCounter{}, testAbusePolicy(t), ch, UnconfiguredHumanChallenge{})
	if err != nil {
		t.Fatal(err)
	}
	out := eng.Evaluate(context.Background(), AbuseSubject{
		Operation: AuthOpSignupComplete, IP: "192.0.2.8", Proof: "proof", ChallengeToken: "anything",
	}, PhaseChallenge)
	if out.Allow() || out.Reason != ReasonProviderUnavailable {
		t.Fatalf("unconfigured: %+v", out)
	}
}
