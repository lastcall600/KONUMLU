package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/eids"
	"backend/internal/eids/trdecision"
	listingcontracts "backend/internal/listings/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
)

func TestIngressAuthAndApply(t *testing.T) {
	fx := newHTTPFixture(t)
	env := fx.issue(trdecision.StatusApproved)
	raw, _ := json.Marshal(env)

	req := httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer wrong-token")
	req.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer-session"})
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cookie+wrong=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer staff-dev-token")
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("staff bearer=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ok=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"idempotent":true`) {
		t.Fatalf("dup=%s", rec.Body.String())
	}

	bad := append([]byte(nil), raw...)
	bad = bytes.Replace(bad, []byte(`"approved"`), []byte(`"rejected"`), 1)
	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(bad))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tamper status=%d", rec.Code)
	}

	tckn := []byte(`{"key_id":"tr-v1","claims":{"schema_version":"tr-decision-v1","verification_type":"property","status":"approved","decision_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","subject_ref":"` + fx.subject + `","issued_at":"2026-09-14T12:00:00Z","valid_until":"2026-09-15T12:00:00Z","audience":"konumlu-germany-eids-v1","tckn":"10000000146"},"signature":"e30"}`)
	req = httptest.NewRequest(http.MethodPost, "/internal/tr-compliance/v1/verification-decisions", bytes.NewReader(tckn))
	req.Header.Set("Authorization", "Bearer "+fx.token)
	rec = httptest.NewRecorder()
	fx.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("tckn=%d", rec.Code)
	}
}

type httpFixture struct {
	h       *Handler
	token   string
	subject string
	key     trdecision.PrivateKey
	now     time.Time
	svc     *eids.Service
}

func newHTTPFixture(t *testing.T) *httpFixture {
	t.Helper()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	store := eids.NewMemoryStore()
	listings := &stubListings{owner: mustID(t), listing: mustID(t), category: mustID(t)}
	policy := &stubPolicy{req: mdcontracts.EIDSRequirementProperty}
	gw := &scriptedGateway{property: eids.ProviderOutcome{Outcome: eids.OutcomeUnavailable}}
	svc, err := eids.NewService(store, listings, policy, gw, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	k, err := trdecision.GeneratePrivateKey("tr-v1")
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := k.Public()
	ring, _ := trdecision.NewPublicRing([]trdecision.PublicKey{pub})
	ver, _ := trdecision.NewVerifier(ring, trdecision.AudienceGermanyV1, trdecision.DefaultTimePolicy(), func() time.Time { return now })
	svc.SetSignedDecisionVerifier(ver)
	v, err := svc.StartForOwner(context.Background(), listings.owner, listings.listing, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.LookupSubjectByVerification(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	token := "tr-ingress-test-token"
	h, err := New(svc, token, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return &httpFixture{h: h, token: token, subject: b.SubjectRef, key: k, now: now, svc: svc}
}

func (fx *httpFixture) issue(st trdecision.Status) trdecision.Envelope {
	iss, _ := trdecision.NewIssuer(fx.key, func() time.Time { return fx.now })
	env, err := iss.Issue(trdecision.ProviderResult{
		SubjectRef:       fx.subject,
		VerificationType: trdecision.TypeProperty,
		Status:           st,
		ValidUntil:       fx.now.Add(24 * time.Hour),
	})
	if err != nil {
		panic(err)
	}
	return env
}

func mustID(t *testing.T) eids.ID {
	t.Helper()
	id, err := eids.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type stubListings struct {
	listing, owner, category eids.ID
}

func (s *stubListings) ResolveListingEIDSSubject(_ context.Context, listingID listingcontracts.ID) (listingcontracts.ListingEIDSSubject, error) {
	if listingcontracts.ID(s.listing) != listingID {
		return listingcontracts.ListingEIDSSubject{}, listingcontracts.ErrNotFound
	}
	return listingcontracts.ListingEIDSSubject{
		ID: listingID, OwnerUserID: listingcontracts.ID(s.owner), CategoryID: listingcontracts.ID(s.category),
	}, nil
}

type stubPolicy struct{ req mdcontracts.EIDSRequirement }

func (s *stubPolicy) Requirement(context.Context, mdcontracts.ID) (mdcontracts.EIDSRequirement, error) {
	return s.req, nil
}

type scriptedGateway struct {
	property eids.ProviderOutcome
}

func (s *scriptedGateway) VerifyProperty(context.Context, eids.PropertyVerifyRequest) (eids.ProviderOutcome, error) {
	return s.property, nil
}
func (s *scriptedGateway) VerifyVehicle(context.Context, eids.VehicleVerifyRequest) (eids.ProviderOutcome, error) {
	return eids.ProviderOutcome{Outcome: eids.OutcomeUnavailable}, nil
}
