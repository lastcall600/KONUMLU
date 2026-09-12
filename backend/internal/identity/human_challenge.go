package identity

import (
	"context"
	"crypto/subtle"
	"strings"
)

// HumanChallengeAction binds a provider token to one auth operation.
type HumanChallengeAction string

// HumanChallengeInput is the server-side verification request.
// Token is never logged and never stored raw.
type HumanChallengeInput struct {
	Token    string
	Action   HumanChallengeAction
	Hostname string
}

// HumanChallengeResult is the provider-neutral verification outcome.
type HumanChallengeResult struct {
	OK       bool
	Action   HumanChallengeAction
	Hostname string
}

// HumanChallenge is the Identity port for human-challenge verification.
// Domain code must not import vendor SDKs or vendor response structs.
type HumanChallenge interface {
	Name() string
	Verify(ctx context.Context, in HumanChallengeInput) (HumanChallengeResult, error)
}

// NewHumanChallengeVerifier returns the AUTH-A verifier for a named mode.
// Production vendor adapters are not selected in this package.
func NewHumanChallengeVerifier(provider string) (HumanChallenge, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", HumanChallengeProviderNone:
		return nil, nil
	case HumanChallengeProviderFake:
		return FakeHumanChallenge{}, nil
	case HumanChallengeProviderUnconfigured:
		return UnconfiguredHumanChallenge{}, nil
	default:
		return nil, errInvalidAbusePolicy
	}
}

// FakeHumanChallenge is a development/test verifier. It is not identity proof.
// AcceptToken defaults to "ok:<action>".
type FakeHumanChallenge struct {
	AcceptToken      string
	ExpectedHostname string
	Err              error
}

func (FakeHumanChallenge) Name() string { return HumanChallengeProviderFake }

func (f FakeHumanChallenge) Verify(ctx context.Context, in HumanChallengeInput) (HumanChallengeResult, error) {
	if err := ctx.Err(); err != nil {
		return HumanChallengeResult{}, err
	}
	if f.Err != nil {
		return HumanChallengeResult{}, f.Err
	}
	action := HumanChallengeAction(strings.TrimSpace(string(in.Action)))
	token := strings.TrimSpace(in.Token)
	host := strings.TrimSpace(in.Hostname)
	out := HumanChallengeResult{Action: action, Hostname: host}
	if token == "" || action == "" {
		return out, nil
	}
	want := strings.TrimSpace(f.AcceptToken)
	if want == "" {
		want = "ok:" + string(action)
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
		return out, nil
	}
	expectedHost := strings.TrimSpace(f.ExpectedHostname)
	if expectedHost != "" && !strings.EqualFold(host, expectedHost) {
		return out, nil
	}
	out.OK = true
	return out, nil
}

// UnconfiguredHumanChallenge is the production-safe stub when no vendor is wired.
// Required challenges must fail closed; this never returns OK.
type UnconfiguredHumanChallenge struct{}

func (UnconfiguredHumanChallenge) Name() string { return HumanChallengeProviderUnconfigured }

func (UnconfiguredHumanChallenge) Verify(context.Context, HumanChallengeInput) (HumanChallengeResult, error) {
	return HumanChallengeResult{}, errUnavailable
}

var (
	_ HumanChallenge = FakeHumanChallenge{}
	_ HumanChallenge = UnconfiguredHumanChallenge{}
)
