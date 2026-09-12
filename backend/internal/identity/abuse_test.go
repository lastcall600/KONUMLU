package identity

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type memCounter struct {
	n    map[string]int64
	keys []string
	err  error
}

func (m *memCounter) Increment(_ context.Context, key string, ttl time.Duration) (int64, error) {
	if ttl <= 0 {
		return 0, errors.New("ttl")
	}
	if m.err != nil {
		return 0, m.err
	}
	if m.n == nil {
		m.n = map[string]int64{}
	}
	m.keys = append(m.keys, key)
	m.n[key]++
	return m.n[key], nil
}

func testAbusePolicy(t *testing.T) AuthAbusePolicy {
	t.Helper()
	p, err := BuildAuthAbusePolicy(
		RateLimitBucket{Max: 2, Window: time.Minute},
		RateLimitBucket{Max: 1, Window: time.Minute},
		RateLimitBucket{Max: 2, Window: time.Minute},
		RateLimitBucket{Max: 1, Window: time.Minute},
		RateLimitBucket{Max: 1, Window: time.Minute},
	)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFormatRateLimitKeyHashesSubject(t *testing.T) {
	email := "owner@example.com"
	key := FormatRateLimitKey(AuthOpSignupStart, DimensionTarget, email)
	if strings.Contains(key, email) || strings.Contains(key, "owner") {
		t.Fatalf("raw identifier in key %q", key)
	}
	if !strings.HasPrefix(key, authRateLimitKeyPrefix+"signup_start:target:") {
		t.Fatalf("key = %q", key)
	}
	phone := "+905551112233"
	ipKey := FormatRateLimitKey(AuthOpPasswordLogin, DimensionIP, "192.0.2.9")
	if strings.Contains(ipKey, "192.0.2.9") || strings.Contains(FormatRateLimitKey(AuthOpResetStart, DimensionTarget, phone), phone) {
		t.Fatal("raw IP or phone in key")
	}
	tgt := PasswordLoginTarget(IdentifierEmail, email)
	tgtKey := FormatRateLimitKey(AuthOpPasswordLogin, DimensionTarget, tgt)
	if strings.Contains(tgtKey, email) || strings.Contains(tgtKey, "email:") {
		t.Fatalf("raw password-login target in key %q", tgtKey)
	}
}

func TestIndependentDimensionsHaveIndependentCounters(t *testing.T) {
	ctx := context.Background()
	inc := &memCounter{}
	eng, err := NewAbuseEngine(inc, testAbusePolicy(t), HumanChallengePolicy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ip := AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1"}
	if !eng.Evaluate(ctx, ip, PhaseIP).Allow() {
		t.Fatal("first IP")
	}
	if !eng.Evaluate(ctx, ip, PhaseIP).Allow() {
		t.Fatal("second IP")
	}
	out := eng.Evaluate(ctx, ip, PhaseIP)
	if out.Allow() || out.Decision != RiskRestrict || out.Reason != ReasonVelocityIP {
		t.Fatalf("third IP = %+v", out)
	}
	account := AbuseSubject{Operation: AuthOpPasswordLogin, IP: "192.0.2.1", AccountID: mustTestID(t)}
	out = eng.Evaluate(ctx, account, PhaseAccount)
	if !out.Allow() {
		t.Fatalf("account dimension must be independent of IP: %+v", out)
	}
}

func TestRateLimitUnavailableFailsClosed(t *testing.T) {
	eng, err := NewAbuseEngine(&memCounter{err: errors.New("dial valkey")}, testAbusePolicy(t), HumanChallengePolicy{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := eng.Evaluate(context.Background(), AbuseSubject{Operation: AuthOpSignupComplete, IP: "10.0.0.1", Proof: "proof"}, PhaseAll)
	if out.Allow() || out.Decision != RiskRestrict || out.Reason != ReasonStorageUnavailable {
		t.Fatalf("out = %+v", out)
	}
}

func TestPasswordLoginTargetMatchesAccountWindow(t *testing.T) {
	p := testAbusePolicy(t)
	if !p.PasswordLogin.Target.enabled() {
		t.Fatal("password login must count an existence-neutral target")
	}
	if p.PasswordLogin.Target != p.PasswordLogin.Account {
		t.Fatalf("password login target %+v must match account %+v", p.PasswordLogin.Target, p.PasswordLogin.Account)
	}
	if p.PasskeyLoginFinish.Target.enabled() {
		t.Fatal("discoverable passkey finish must not enable identifier target")
	}
	if p.SignupFinish.Target.Max == p.PasswordLogin.Account.Max {
		t.Fatal("verify-by-challenge target must stay independent of password-login identifier target")
	}
}

func TestDeviceDimensionStaysUnused(t *testing.T) {
	p := testAbusePolicy(t)
	if p.PasswordLogin.Device.enabled() || p.SignupComplete.Device.enabled() {
		t.Fatal("device dimension must stay unused")
	}
	p.PasswordLogin.Device = RateLimitBucket{Max: 3, Window: time.Minute}
	if err := p.Validate(); err != errInvalidAbusePolicy {
		t.Fatalf("enabled device: %v", err)
	}
}

func TestNilEngineUnavailable(t *testing.T) {
	if _, err := NewAbuseEngine(nil, testAbusePolicy(t), HumanChallengePolicy{}, nil); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v", err)
	}
	var eng *AbuseEngine
	out := eng.Evaluate(context.Background(), AbuseSubject{Operation: AuthOpPasswordLogin}, PhaseIP)
	if out.Allow() {
		t.Fatal("nil engine must not allow")
	}
}

func mustTestID(t *testing.T) ID {
	t.Helper()
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
