package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	eidscontracts "backend/internal/eids/contracts"
	"backend/internal/listings"
	"backend/internal/masterdata/contracts"
)

func TestPublishFailsClosedWithoutPolicy(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(nil, staticEIDSGate{verified: true}, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCategoryWithoutEIDSPublishesAsBefore(t *testing.T) {
	h := newTestHandler(t)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPropertyRequiredCannotPublishWithoutVerified(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, staticEIDSGate{verified: false}, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, map[string]any{"eligible": true}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := h.listingStore.Get(context.Background(), ready.ID)
	if err != nil || stored.Status != listings.StatusReady {
		t.Fatalf("must not publish: %+v err=%v", stored, err)
	}
}

func TestVehicleRequiredCannotPublishWithoutVerified(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementVehicle}, staticEIDSGate{verified: false}, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestVerifiedAllowsPublishEligibility(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, staticEIDSGate{verified: true}, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProviderUnavailableBlocksPublish(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, staticEIDSGate{verified: false}, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestMissingGateForRequiredCategoryIsNotEligible(t *testing.T) {
	h := newTestHandler(t)
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementVehicle}, nil, nil)
	ready := markReady(t, h, createDraft(t, h))
	rec := do(t, h, http.MethodPost, "/v1/listings/"+ready.ID.String()+"/publish", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestEIDSStartWrongTypeRejected(t *testing.T) {
	h := newTestHandler(t)
	owner := &stubEIDSOwner{err: eidscontracts.ErrWrongType}
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, nil, owner)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/eids-verifications", allowedOrigin, map[string]any{
		"verificationType": "vehicle",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEIDSStartClientCannotSpoofStatusOrEligibility(t *testing.T) {
	h := newTestHandler(t)
	owner := &stubEIDSOwner{}
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, nil, owner)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/eids-verifications", allowedOrigin, map[string]any{
		"status":            "verified",
		"providerReference": "secret-ref",
		"eligible":          true,
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if owner.starts != 0 {
		t.Fatal("must not start when client spoofs result fields")
	}
}

func TestEIDSStartIdempotentAndOwnerOnly(t *testing.T) {
	h := newTestHandler(t)
	owner := &stubEIDSOwner{kind: eidscontracts.TypeProperty}
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementProperty}, nil, owner)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/eids-verifications", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var first eidsDTO
	decode(t, rec, &first)
	if strings.Contains(rec.Body.String(), "providerReference") || strings.Contains(strings.ToLower(rec.Body.String()), "tckn") {
		t.Fatalf("payload leaked: %s", rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/eids-verifications", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat status = %d", rec.Code)
	}
	var second eidsDTO
	decode(t, rec, &second)
	if first.VerificationID != second.VerificationID {
		t.Fatalf("idempotent id %s vs %s", first.VerificationID, second.VerificationID)
	}

	h.sessions.userID = mustID(t)
	rec = do(t, h, http.MethodPost, "/v1/listings/"+created.ID.String()+"/eids-verifications", allowedOrigin, nil, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/listings/"+created.ID.String()+"/eids-verification", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("stranger get status = %d", rec.Code)
	}
}

func TestEIDSGetOmitsProviderReference(t *testing.T) {
	h := newTestHandler(t)
	owner := &stubEIDSOwner{kind: eidscontracts.TypeVehicle, status: eidscontracts.StatusVerified}
	h.SetEIDS(staticEIDSPolicy{req: contracts.EIDSRequirementVehicle}, staticEIDSGate{verified: true}, owner)
	created := createDraft(t, h)
	rec := do(t, h, http.MethodGet, "/v1/listings/"+created.ID.String()+"/eids-verification", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["providerReference"]; ok {
		t.Fatal("providerReference must not be public")
	}
}

type stubEIDSOwner struct {
	kind   eidscontracts.VerificationType
	status eidscontracts.Status
	err    error
	view   eidscontracts.VerificationView
	starts int
}

func (s *stubEIDSOwner) StartForOwner(_ context.Context, _, listingID eidscontracts.ID, _ string) (eidscontracts.VerificationView, error) {
	s.starts++
	if s.err != nil {
		return eidscontracts.VerificationView{}, s.err
	}
	return s.ensure(listingID), nil
}

func (s *stubEIDSOwner) CurrentForOwner(_ context.Context, _, listingID eidscontracts.ID) (eidscontracts.VerificationView, error) {
	if s.err != nil {
		return eidscontracts.VerificationView{}, s.err
	}
	return s.ensure(listingID), nil
}

func (s *stubEIDSOwner) ensure(listingID eidscontracts.ID) eidscontracts.VerificationView {
	if !s.view.VerificationID.IsZero() {
		return s.view
	}
	now := time.Date(2026, 9, 10, 1, 0, 0, 0, time.UTC)
	id := eidscontracts.ID{}
	id[0] = 9
	kind := s.kind
	if kind == "" {
		kind = eidscontracts.TypeProperty
	}
	status := s.status
	if status == "" {
		status = eidscontracts.StatusPending
	}
	s.view = eidscontracts.VerificationView{
		VerificationID:   id,
		ListingID:        listingID,
		VerificationType: kind,
		Status:           status,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return s.view
}
