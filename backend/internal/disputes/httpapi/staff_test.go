package httpapi

import (
	"context"
	"net/http"
	"testing"

	"backend/internal/disputes"
	staffauth "backend/internal/staffauth/contracts"
)

func TestConsumerRegisterDoesNotExposeStaffDisputes(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	id := mustDispID(t).String()
	rec := do(t, h, http.MethodGet, "/v1/staff/disputes/"+id, "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("staff get status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodPost, "/v1/staff/disputes/"+id+"/review", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("staff review status = %d", rec.Code)
	}
}

func TestStaffAuthorizerAndInternalReviewDoesNotRefund(t *testing.T) {
	h := newFixture(t)
	h.sessions.userID = h.requester
	rec := do(t, h, http.MethodPost, "/v1/transactions/"+h.txnID.String()+"/dispute", allowedOrigin, map[string]any{"reasonCode": "other"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var created disputeDTO
	decode(t, rec, &created)
	disputeID, err := disputes.ParseID(created.DisputeID)
	if err != nil {
		t.Fatal(err)
	}

	unauth, err := NewStaff(stubStaff{err: ErrUnauthenticated}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, unauth, http.MethodGet, "/v1/staff/disputes/"+created.DisputeID, allowedOrigin, nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", rec.Code)
	}
	consumer, err := NewStaff(stubStaff{err: ErrNotStaff}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, consumer, http.MethodPost, "/v1/staff/disputes/"+created.DisputeID+"/review", allowedOrigin, map[string]any{}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff status = %d", rec.Code)
	}

	staffID := mustDispID(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: staffID}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, sh, http.MethodPost, "/v1/staff/disputes/"+created.DisputeID+"/review", allowedOrigin, map[string]any{}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("review status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, sh, http.MethodPost, "/v1/staff/disputes/"+created.DisputeID+"/evidence", allowedOrigin, map[string]any{
		"title":          "internal note",
		"referenceValue": "case-file-1",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("evidence status = %d body=%s", rec.Code, rec.Body.String())
	}
	list, err := h.svc.ListEvidenceInternal(context.Background(), disputeID)
	if err != nil || len(list) == 0 {
		t.Fatalf("evidence list = %+v err=%v", list, err)
	}
	found := false
	for _, e := range list {
		if e.ActorRole == disputes.RoleInternal && e.ActorUserID != nil && *e.ActorUserID == staffID {
			found = true
		}
	}
	if !found {
		t.Fatalf("staff actor missing on internal evidence: %+v", list)
	}
	rec = do(t, sh, http.MethodPost, "/v1/staff/disputes/"+created.DisputeID+"/resolve", allowedOrigin, map[string]any{"resolutionCode": "no_action"}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resolved staffDisputeDTO
	decode(t, rec, &resolved)
	if resolved.Status != string(disputes.StatusResolved) || resolved.ResolutionCode == nil || *resolved.ResolutionCode != string(disputes.ResolutionNoAction) {
		t.Fatalf("resolved = %+v", resolved)
	}
	got, err := h.svc.GetInternal(context.Background(), disputeID)
	if err != nil || got.Status != disputes.StatusResolved {
		t.Fatalf("internal get = %+v err=%v", got, err)
	}

	moderator, err := NewStaff(stubStaff{
		actor: StaffActor{ID: staffID},
		deny:  map[staffauth.Permission]bool{staffauth.PermDisputesReview: true},
	}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, moderator, http.MethodPost, "/v1/staff/disputes/"+created.DisputeID+"/close", allowedOrigin, map[string]any{}, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("moderator close status = %d", rec.Code)
	}
}

type stubStaff struct {
	actor StaffActor
	err   error
	deny  map[staffauth.Permission]bool
}

func (s stubStaff) Authenticate(*http.Request) (staffauth.Principal, error) {
	if s.err != nil {
		return staffauth.Principal{}, s.err
	}
	var id staffauth.ID
	copy(id[:], s.actor.ID[:])
	return staffauth.Principal{StaffID: id}, nil
}

func (s stubStaff) Allows(p staffauth.Principal, perm staffauth.Permission) bool {
	if s.err != nil {
		return false
	}
	if s.deny[perm] {
		return false
	}
	return !p.StaffID.IsZero() || !s.actor.ID.IsZero()
}
