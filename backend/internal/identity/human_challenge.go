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

// MaxHumanChallengeTokenLength is the maximum accepted challenge token size.
const MaxHumanChallengeTokenLength = 2048

// HumanChallengeResult is the provider-neutral verification outcome.
type HumanChallengeResult struct {
	OK           bool
	Action       HumanChallengeAction
	Hostname     string
	FailureClass string
}

// HumanChallenge is the Identity port for human-challenge verification.
// Domain code must not import vendor SDKs or vendor response structs.
//
// Production adapter contract:
//   - token is verified server-side; frontend tokens are never trusted alone
//   - expected action is the server AuthOperation; the client cannot set it
//   - expected hostname/site is bound against an exact allowlist
//   - timeout, invalid token, malformed response, wrong action, and wrong
//     hostname must not return OK
//   - replay is provider single-use and Identity hashed-token replay
//   - secrets and tokens are never logged
//   - provider errors map to unavailable / not-OK; required challenge is fail-closed
//
// Cloudflare Turnstile is constructed in infrastructure, not here.
// HumanChallenge is not identity verification and not Step-Up.
type HumanChallenge interface {
	Name() string
	Verify(ctx context.Context, in HumanChallengeInput) (HumanChallengeResult, error)
}

// NewHumanChallengeVerifier returns built-in verifiers. Turnstile is wired
// at process start from infrastructure (stdlib Siteverify, no SDK).
func NewHumanChallengeVerifier(provider string) (HumanChallenge, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", HumanChallengeProviderNone:
		return nil, nil
	case HumanChallengeProviderFake:
		return FakeHumanChallenge{}, nil
	case HumanChallengeProviderUnconfigured:
		return UnconfiguredHumanChallenge{}, nil
	case HumanChallengeProviderTurnstile:
		return nil, errInvalidAbusePolicy
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
