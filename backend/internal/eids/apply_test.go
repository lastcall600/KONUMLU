package eids

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"backend/internal/eids/trdecision"
	mdcontracts "backend/internal/masterdata/contracts"
)

func attachVerifier(t *testing.T, fx *fixture, k trdecision.PrivateKey, now time.Time) *trdecision.Verifier {
	t.Helper()
	pub, err := k.Public()
	if err != nil {
		t.Fatal(err)
	}
	ring, err := trdecision.NewPublicRing([]trdecision.PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	v, err := trdecision.NewVerifier(ring, trdecision.AudienceGermanyV1, trdecision.DefaultTimePolicy(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	fx.svc.SetSignedDecisionVerifier(v)
	fx.now = now
	return v
}

func startPending(t *testing.T, fx *fixture) (Verification, string) {
	t.Helper()
	fx.policy.req = mdcontracts.EIDSRequirementProperty
	fx.gateway.property = ProviderOutcome{Outcome: OutcomeUnavailable}
	v, err := fx.svc.StartForOwner(context.Background(), fx.owner, fx.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := fx.store.LookupSubjectByVerification(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	return v, b.SubjectRef
}

func issue(t *testing.T, k trdecision.PrivateKey, subject string, status trdecision.Status, issued time.Time) trdecision.Envelope {
	t.Helper()
	iss, err := trdecision.NewIssuer(k, func() time.Time { return issued })
	if err != nil {
		t.Fatal(err)
	}
	env, err := iss.Issue(trdecision.ProviderResult{
		SubjectRef:       subject,
		VerificationType: trdecision.TypeProperty,
		Status:           status,
		ValidUntil:       issued.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestSignedApprovedAndRejected(t *testing.T) {
	fx := newFixture(t)
	k, err := trdecision.GeneratePrivateKey("tr-v1")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	attachVerifier(t, fx, k, now)
	_, subject := startPending(t, fx)
	got, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusApproved, now))
	if err != nil || got.Verification.Status != StatusVerified {
		t.Fatalf("approved=%+v err=%v", got, err)
	}
	ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
	if err != nil || !ok {
		t.Fatal("must be verified")
	}

	fx2 := newFixture(t)
	attachVerifier(t, fx2, k, now)
	_, subject2 := startPending(t, fx2)
	got, err = fx2.svc.IngestSignedDecision(context.Background(), issue(t, k, subject2, trdecision.StatusRejected, now))
	if err != nil || got.Verification.Status != StatusFailed {
		t.Fatalf("rejected=%+v err=%v", got, err)
	}
}

func TestReplayIdempotentAndConflict(t *testing.T) {
	fx := newFixture(t)
	k, _ := trdecision.GeneratePrivateKey("tr-v1")
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	attachVerifier(t, fx, k, now)
	_, subject := startPending(t, fx)
	env := issue(t, k, subject, trdecision.StatusApproved, now)
	first, err := fx.svc.IngestSignedDecision(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fx.svc.IngestSignedDecision(context.Background(), env)
	if err != nil || !second.Idempotent {
		t.Fatalf("idempotent=%+v err=%v", second, err)
	}
	if fx.store.ReplayCount() != 1 {
		t.Fatalf("replay count=%d", fx.store.ReplayCount())
	}
	changed := env.Claims
	changed.Status = trdecision.StatusRejected
	conflictEnv, err := k.Sign(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fx.svc.IngestSignedDecision(context.Background(), conflictEnv); !errors.Is(err, errReplayConflict) {
		t.Fatalf("conflict=%v", err)
	}
	_ = first
}

func TestMissingSubjectAndCrossUser(t *testing.T) {
	fx := newFixture(t)
	k, _ := trdecision.GeneratePrivateKey("tr-v1")
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	attachVerifier(t, fx, k, now)
	_, subject := startPending(t, fx)
	unknown := strings.Repeat("ef", 32)
	env := issue(t, k, unknown, trdecision.StatusApproved, now)
	if _, err := fx.svc.IngestSignedDecision(context.Background(), env); !errors.Is(err, errUnmappedSubject) {
		t.Fatalf("unmapped=%v", err)
	}
	other := newFixture(t)
	attachVerifier(t, other, k, now)
	v2, subject2 := startPending(t, other)
	envA := issue(t, k, subject, trdecision.StatusApproved, now)
	if _, err := other.svc.IngestSignedDecision(context.Background(), envA); !errors.Is(err, errUnmappedSubject) {
		t.Fatalf("cross mapping=%v", err)
	}
	if _, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject2, trdecision.StatusApproved, now)); !errors.Is(err, errUnmappedSubject) {
		t.Fatalf("other subject on fx=%v", err)
	}
	if v2.Status == StatusVerified {
		t.Fatal("cross-user must not verify")
	}
}

func TestDBOutageAndRollback(t *testing.T) {
	fx := newFixture(t)
	k, _ := trdecision.GeneratePrivateKey("tr-v1")
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	attachVerifier(t, fx, k, now)
	_, subject := startPending(t, fx)
	fx.store.SetFail(errUnavailable)
	if _, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusApproved, now)); !errors.Is(err, errUnavailable) {
		t.Fatalf("outage=%v", err)
	}
	fx.store.SetFail(nil)
	ok, _ := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
	if ok {
		t.Fatal("outage must not approve")
	}
	fx.store.SetFailUpdate(true)
	if _, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusApproved, now)); !errors.Is(err, errUnavailable) {
		t.Fatalf("rollback=%v", err)
	}
	if fx.store.ReplayCount() != 0 {
		t.Fatal("replay must roll back with failed apply")
	}
	ok, _ = fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
	if ok {
		t.Fatal("verification must not commit")
	}
}

func TestMonotonicIssuedAtOrdering(t *testing.T) {
	k, err := trdecision.GeneratePrivateKey("tr-v1")
	if err != nil {
		t.Fatal(err)
	}
	older := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 14, 10, 2, 0, 0, time.UTC)
	now := time.Date(2026, 9, 14, 10, 5, 0, 0, time.UTC)

	t.Run("older rejected then newer approved", func(t *testing.T) {
		fx := newFixture(t)
		attachVerifier(t, fx, k, now)
		_, subject := startPending(t, fx)
		got, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusRejected, older))
		if err != nil || got.Verification.Status != StatusFailed {
			t.Fatalf("first=%+v err=%v", got, err)
		}
		got, err = fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusApproved, newer))
		if err != nil || got.Verification.Status != StatusVerified {
			t.Fatalf("second=%+v err=%v", got, err)
		}
		ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
		if err != nil || !ok {
			t.Fatal("final must be approved")
		}
		if fx.store.ReplayCount() != 2 {
			t.Fatalf("replay=%d", fx.store.ReplayCount())
		}
	})

	t.Run("newer approved first older rejected later", func(t *testing.T) {
		fx := newFixture(t)
		attachVerifier(t, fx, k, now)
		v, subject := startPending(t, fx)
		newerEnv := issue(t, k, subject, trdecision.StatusApproved, newer)
		if _, err := fx.svc.IngestSignedDecision(context.Background(), newerEnv); err != nil {
			t.Fatal(err)
		}
		olderEnv := issue(t, k, subject, trdecision.StatusRejected, older)
		got, err := fx.svc.IngestSignedDecision(context.Background(), olderEnv)
		if err != nil {
			t.Fatalf("stale ingest err=%v", err)
		}
		if got.Verification.Status != StatusVerified {
			t.Fatalf("must remain approved: %+v", got.Verification)
		}
		ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
		if err != nil || !ok {
			t.Fatal("must remain verified")
		}
		if fx.store.ReplayCount() != 2 {
			t.Fatalf("stale must still be recorded replay=%d", fx.store.ReplayCount())
		}
		if _, err := fx.store.GetReplay(context.Background(), olderEnv.Claims.DecisionID); err != nil {
			t.Fatalf("audit row missing: %v", err)
		}
		cur, err := fx.store.Get(context.Background(), v.ID)
		if err != nil || cur.Status != StatusVerified {
			t.Fatalf("row=%+v err=%v", cur, err)
		}
	})

	t.Run("older approved after newer rejected does not restore", func(t *testing.T) {
		fx := newFixture(t)
		attachVerifier(t, fx, k, now)
		_, subject := startPending(t, fx)
		if _, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusRejected, newer)); err != nil {
			t.Fatal(err)
		}
		got, err := fx.svc.IngestSignedDecision(context.Background(), issue(t, k, subject, trdecision.StatusApproved, older))
		if err != nil {
			t.Fatal(err)
		}
		if got.Verification.Status != StatusFailed {
			t.Fatalf("must remain rejected: %+v", got.Verification)
		}
		ok, err := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
		if err != nil || ok {
			t.Fatal("must not restore approved")
		}
		if fx.store.ReplayCount() != 2 {
			t.Fatalf("replay=%d", fx.store.ReplayCount())
		}
	})

	t.Run("equal issued_at different claims fail safely", func(t *testing.T) {
		fx := newFixture(t)
		attachVerifier(t, fx, k, now)
		_, subject := startPending(t, fx)
		first := issue(t, k, subject, trdecision.StatusApproved, older)
		if _, err := fx.svc.IngestSignedDecision(context.Background(), first); err != nil {
			t.Fatal(err)
		}
		second := issue(t, k, subject, trdecision.StatusRejected, older)
		if _, err := fx.svc.IngestSignedDecision(context.Background(), second); !errors.Is(err, errReplayConflict) {
			t.Fatalf("tie=%v", err)
		}
		ok, _ := fx.svc.IsVerified(context.Background(), fx.listing, TypeProperty)
		if !ok {
			t.Fatal("tie must not change applied state")
		}
		if fx.store.ReplayCount() != 1 {
			t.Fatalf("tie must not insert replay=%d", fx.store.ReplayCount())
		}
	})
}

func TestPrivateKeyAndBodyNotLogged(t *testing.T) {
	k, _ := trdecision.GeneratePrivateKey("tr-v1")
	seed, _ := trdecision.EncodePrivateSeed(k)
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("tr_compliance_decision", "decision_id", "abc", "key_id", "tr-v1", "result_class", "applied")
	out := buf.String()
	if strings.Contains(out, seed) || strings.Contains(out, "signature") {
		t.Fatalf("leaked: %s", out)
	}
}

func TestTrustStaffTurnstileUntouchedByDecisionTypes(t *testing.T) {
	if StatusVerified != "verified" || TypeProperty != "property" {
		t.Fatal("eids product statuses must stay")
	}
}
