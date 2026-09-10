package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/moderation"
	staffauthimpl "backend/internal/staffauth"
	staffauth "backend/internal/staffauth/contracts"
)

func TestConsumerRegisterDoesNotExposeStaffQueue(t *testing.T) {
	h := newTestHandler(t)
	rec := do(t, h, http.MethodGet, "/v1/staff/moderation/reports", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/staff/moderation/cases", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cases status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/cases", allowedOrigin, map[string]any{"title": "nope"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer cases status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer staff evidence status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/cases/"+mustID(t).String()+"/evidence", allowedOrigin, map[string]any{"title": "nope"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer evidence status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/actions", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer staff actions status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/cases/"+mustID(t).String()+"/actions", allowedOrigin, map[string]any{"actionType": "warning"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer actions status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/appeals", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer staff appeals status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/appeals/"+mustID(t).String()+"/status", allowedOrigin, map[string]any{"status": "under_review"}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("consumer staff appeal status path = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMineOmitsStaffNoteAfterTransition(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created reportDTO
	decode(t, rec, &created)
	id, err := moderation.ParseID(created.ReportID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Transition(context.Background(), id, moderation.TransitionInput{
		To:        moderation.StatusTriaged,
		StaffNote: "internal only",
	}); err != nil {
		t.Fatal(err)
	}

	rec = do(t, h, http.MethodGet, "/v1/moderation/reports/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("mine status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	rows := raw["reports"].([]any)
	first := rows[0].(map[string]any)
	for _, key := range []string{"staffNote", "staff_note", "reporterUserId", "statusChangedBy"} {
		if _, present := first[key]; present {
			t.Fatalf("leaked %s: %#v", key, first)
		}
	}
	if first["status"] != "triaged" {
		t.Fatalf("status = %#v", first["status"])
	}
}

type stubStaff struct {
	actor StaffActor
	err   error
	deny  map[staffauth.Permission]bool
}

func (s stubStaff) Authenticate(_ *http.Request) (staffauth.Principal, error) {
	if s.err != nil {
		return staffauth.Principal{}, s.err
	}
	return staffPrincipal(s.actor), nil
}

func (s stubStaff) Allows(p staffauth.Principal, perm staffauth.Permission) bool {
	if s.err != nil {
		return false
	}
	if s.deny[perm] {
		return false
	}
	if p.StaffID.IsZero() && s.actor.ID.IsZero() {
		return false
	}
	return true
}

func TestStaffQueueUnauthorizedAndNonStaffDenied(t *testing.T) {
	h := newTestHandler(t)
	unauth, err := NewStaff(stubStaff{err: ErrUnauthenticated}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, unauth, http.MethodGet, "/v1/staff/moderation/reports", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	rec = staffDo(t, unauth, http.MethodGet, "/v1/staff/moderation/cases", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth cases status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	rec = staffDo(t, unauth, http.MethodPost, "/v1/staff/moderation/cases", map[string]any{
		"subjectType": "listing",
		"subjectId":   mustID(t).String(),
		"title":       "blocked",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth create case status = %d", rec.Code)
	}
	rec = staffDo(t, unauth, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth evidence list status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")
	rec = staffDo(t, unauth, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", map[string]any{
		"evidenceType": "staff_note",
		"title":        "blocked",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth evidence create status = %d", rec.Code)
	}
	rec = staffDo(t, unauth, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/actions", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth actions list status = %d", rec.Code)
	}
	rec = staffDo(t, unauth, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/actions", map[string]any{
		"targetType": "listing",
		"targetId":   mustID(t).String(),
		"actionType": "warning",
		"reasonCode": "policy_violation",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth action create status = %d", rec.Code)
	}

	consumer, err := NewStaff(stubStaff{err: ErrNotStaff}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, consumer, http.MethodGet, "/v1/staff/moderation/reports", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
	rec = staffDo(t, consumer, http.MethodGet, "/v1/staff/moderation/cases", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff cases status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
	rec = staffDo(t, consumer, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff evidence list status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "forbidden")
	rec = staffDo(t, consumer, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", map[string]any{
		"evidenceType": "staff_note",
		"title":        "blocked",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff evidence create status = %d", rec.Code)
	}
	rec = staffDo(t, consumer, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/actions", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff actions list status = %d", rec.Code)
	}
	rec = staffDo(t, consumer, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/actions", map[string]any{
		"targetType": "listing",
		"targetId":   mustID(t).String(),
		"actionType": "warning",
		"reasonCode": "policy_violation",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff action create status = %d", rec.Code)
	}
	rec = staffDo(t, unauth, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/appeals", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth appeals list status = %d", rec.Code)
	}
	rec = staffDo(t, consumer, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/appeals", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff appeals list status = %d", rec.Code)
	}
	rec = staffDo(t, consumer, http.MethodPost, "/v1/staff/moderation/cases/"+mustID(t).String()+"/appeals/"+mustID(t).String()+"/status", map[string]any{
		"status": "under_review",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-staff appeal transition status = %d", rec.Code)
	}
}

func TestStaffListGetAndValidTransition(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: staffID}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created reportDTO
	decode(t, rec, &created)

	rec = staffDo(t, sh, http.MethodGet, "/v1/staff/moderation/reports?status=submitted&order=newest&limit=20", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list staffReportListDTO
	decode(t, rec, &list)
	if len(list.Reports) != 1 || list.Reports[0].ReportID != created.ReportID {
		t.Fatalf("list = %+v", list)
	}
	rawBytes := rec.Body.Bytes()
	var raw map[string]any
	if err := json.Unmarshal(rawBytes, &raw); err != nil {
		t.Fatal(err)
	}
	first := raw["reports"].([]any)[0].(map[string]any)
	if _, present := first["reporterUserId"]; present {
		t.Fatalf("staff DTO leaked reporterUserId: %#v", first)
	}

	rec = staffDo(t, sh, http.MethodGet, "/v1/staff/moderation/reports/"+created.ReportID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = staffDo(t, sh, http.MethodPost, "/v1/staff/moderation/reports/"+created.ReportID+"/status", map[string]any{
		"status":    "triaged",
		"staffNote": "looks like ads",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("triage status = %d body=%s", rec.Code, rec.Body.String())
	}
	var triaged staffReportDTO
	decode(t, rec, &triaged)
	if triaged.Status != "triaged" || triaged.StaffNote == nil || *triaged.StaffNote != "looks like ads" {
		t.Fatalf("triaged = %+v", triaged)
	}
	if triaged.StatusChangedBy == nil || *triaged.StatusChangedBy != staffID.String() {
		t.Fatalf("actor = %+v", triaged.StatusChangedBy)
	}

	rec = staffDo(t, sh, http.MethodPost, "/v1/staff/moderation/reports/"+created.ReportID+"/status", map[string]any{
		"status": "closed",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("close status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = staffDo(t, sh, http.MethodPost, "/v1/staff/moderation/reports/"+created.ReportID+"/status", map[string]any{
		"status": "submitted",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("invalid jump status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "conflict")
}

func TestStaffCaseEvidenceCreateListAndAuthActor(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: staffID}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := h.svc.CreateCase(context.Background(), moderation.CreateCaseInput{
		SubjectType: moderation.TargetListing, SubjectID: mustID(t), Title: "staff evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/staff/moderation/cases/" + detail.Case.ID.String() + "/evidence"
	rec := staffDo(t, sh, http.MethodPost, path, map[string]any{
		"evidenceType": "staff_note",
		"title":        "intake",
		"description":  "internal only",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create note status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created staffEvidenceDTO
	decode(t, rec, &created)
	if created.EvidenceType != "staff_note" || created.ActorStaffID == nil || *created.ActorStaffID != staffID.String() {
		t.Fatalf("created = %+v", created)
	}
	rec = staffDo(t, sh, http.MethodPost, path, map[string]any{
		"evidenceType":   "external_reference",
		"title":          "source",
		"referenceValue": "https://example.test/ref",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create ref status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list staffEvidenceListDTO
	decode(t, rec, &list)
	if len(list.Evidence) != 2 {
		t.Fatalf("list = %+v", list)
	}
	foundCreated := false
	for _, row := range list.Evidence {
		if row.EvidenceID == created.EvidenceID {
			foundCreated = true
			break
		}
	}
	if !foundCreated {
		t.Fatalf("list = %+v", list)
	}
	raw := rec.Body.Bytes()
	if strings.Contains(string(raw), "reporter") || strings.Contains(string(raw), "email") {
		t.Fatalf("leaked reporter identity: %s", raw)
	}
	rec = staffDo(t, sh, http.MethodPost, path, map[string]any{
		"evidenceType":   "staff_note",
		"title":          "bad",
		"referenceValue": "https://example.test",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid fields status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodGet, "/v1/staff/moderation/cases/"+mustID(t).String()+"/evidence", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing case status = %d", rec.Code)
	}
}

func TestStaffCaseActionsCreateListTransitionAndAuthActor(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: staffID}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	subject := h.publishListing(t, h.other)
	detail, err := h.svc.CreateCase(context.Background(), moderation.CreateCaseInput{
		SubjectType: moderation.TargetListing, SubjectID: subject, Title: "staff actions",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/staff/moderation/cases/" + detail.Case.ID.String() + "/actions"
	rec := staffDo(t, sh, http.MethodPost, base, map[string]any{
		"targetType": "listing",
		"targetId":   subject.String(),
		"actionType": "warning",
		"reasonCode": "policy_violation",
		"rationale":  "internal only",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create action status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created staffActionDTO
	decode(t, rec, &created)
	if created.Status != "proposed" || created.ActorStaffID == nil || *created.ActorStaffID != staffID.String() {
		t.Fatalf("created = %+v", created)
	}
	rec = staffDo(t, sh, http.MethodGet, base, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list staffActionListDTO
	decode(t, rec, &list)
	if len(list.Actions) != 1 || list.Actions[0].ActionID != created.ActionID {
		t.Fatalf("list = %+v", list)
	}
	if strings.Contains(rec.Body.String(), "reporter") {
		t.Fatalf("leaked reporter identity: %s", rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodGet, base+"/"+created.ActionID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{
		"status": "approved",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{
		"status":          "executed",
		"recipientUserId": mustID(t).String(),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client recipient status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{
		"status": "executed",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("execute status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{
		"status": "cancelled",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("executed terminal status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base, map[string]any{
		"targetType":   "listing",
		"targetId":     subject.String(),
		"actionType":   "remove",
		"reasonCode":   "safety_risk",
		"actorStaffId": mustID(t).String(),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client staff id status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base, map[string]any{
		"targetType": "public_profile",
		"targetId":   mustID(t).String(),
		"actionType": "remove",
		"reasonCode": "safety_risk",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatch status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaffAppealsReviewUsesAuthorizerIdentity(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: staffID}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	listing := h.publishListing(t, h.sessions.userID)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)
	submitted, err := h.svc.SubmitAppeal(t.Context(), h.sessions.userID, moderation.CreateAppealInput{
		ActionID: action.ID, Statement: "please reverse record only",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/staff/moderation/cases/" + submitted.CaseID.String() + "/appeals"
	rec := staffDo(t, sh, http.MethodGet, base, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodGet, base+"/"+submitted.ID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+submitted.ID.String()+"/status", map[string]any{
		"status":           "under_review",
		"decidedByStaffId": mustID(t).String(),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("client staff id status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+submitted.ID.String()+"/status", map[string]any{
		"status": "under_review",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("review status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+submitted.ID.String()+"/status", map[string]any{
		"status": "accepted",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d body=%s", rec.Code, rec.Body.String())
	}
	var accepted staffAppealDTO
	decode(t, rec, &accepted)
	if accepted.Status != "accepted" || accepted.DecidedByStaffID == nil || *accepted.DecidedByStaffID != staffID.String() {
		t.Fatalf("accepted = %+v", accepted)
	}
	still, err := h.svc.GetAction(t.Context(), submitted.CaseID, action.ID)
	if err != nil || still.Status != moderation.ActionStatusExecuted {
		t.Fatalf("accept reversed action = %+v err=%v", still, err)
	}
	rec = staffDo(t, sh, http.MethodPost, base+"/"+submitted.ID.String()+"/status", map[string]any{
		"status": "rejected",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("reopen status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaffRejectsImmutableFieldMutation(t *testing.T) {
	h := newTestHandler(t)
	sh, err := NewStaff(stubStaff{actor: StaffActor{ID: mustID(t)}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d", rec.Code)
	}
	var created reportDTO
	decode(t, rec, &created)
	rec = staffDo(t, sh, http.MethodPost, "/v1/staff/moderation/reports/"+created.ReportID+"/status", map[string]any{
		"status":         "triaged",
		"reasonCode":     "harassment",
		"reporterUserId": mustID(t).String(),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaffModeratorCannotApproveOrReviewAppeal(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	sh, err := NewStaff(stubStaff{
		actor: StaffActor{ID: staffID},
		deny: map[staffauth.Permission]bool{
			staffauth.PermModerationActionApprove: true,
			staffauth.PermModerationAppealReview:  true,
		},
	}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	subject := h.publishListing(t, h.other)
	detail, err := h.svc.CreateCase(context.Background(), moderation.CreateCaseInput{
		SubjectType: moderation.TargetListing, SubjectID: subject, Title: "perm",
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "/v1/staff/moderation/cases/" + detail.Case.ID.String() + "/actions"
	rec := staffDo(t, sh, http.MethodPost, base, map[string]any{
		"targetType": "listing",
		"targetId":   subject.String(),
		"actionType": "warning",
		"reasonCode": "policy_violation",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create action status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created staffActionDTO
	decode(t, rec, &created)
	rec = staffDo(t, sh, http.MethodPost, base+"/"+created.ActionID+"/status", map[string]any{"status": "approved"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("approve status = %d body=%s", rec.Code, rec.Body.String())
	}

	listing := h.publishListing(t, h.sessions.userID)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)
	submitted, err := h.svc.SubmitAppeal(t.Context(), h.sessions.userID, moderation.CreateAppealInput{
		ActionID: action.ID, Statement: "please reverse record only",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, sh, http.MethodPost, "/v1/staff/moderation/cases/"+submitted.CaseID.String()+"/appeals/"+submitted.ID.String()+"/status", map[string]any{
		"status": "under_review",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("appeal review status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestStaffRealAuthorizerIgnoresConsumerSessionAndPrivilegeHeaders(t *testing.T) {
	h := newTestHandler(t)
	staffID := mustID(t)
	var sid staffauth.ID
	copy(sid[:], staffID[:])
	authz, err := staffauthimpl.NewAuthorizer(staticStaffProvider{principal: staffauth.Principal{
		StaffID: sid,
		Roles:   []staffauth.Role{staffauth.RoleModerator},
	}}, staffauthimpl.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	sh, err := NewStaff(authz, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	listing := h.publishListing(t, h.other)
	rec := do(t, h, http.MethodPost, "/v1/moderation/reports", allowedOrigin, validListingReport(listing), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("create status = %d", rec.Code)
	}
	var created reportDTO
	decode(t, rec, &created)

	rec = staffDo(t, sh, http.MethodGet, "/v1/staff/moderation/reports/"+created.ReportID, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no bearer status = %d", rec.Code)
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports/"+created.ReportID, nil)
	r.AddCookie(&http.Cookie{Name: "__Host-konumlu_session", Value: "consumer"})
	r.Header.Set("X-Staff-Role", "admin")
	rec = staffServe(t, sh, r)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("consumer session status = %d", rec.Code)
	}

	r = httptest.NewRequest(http.MethodGet, "/v1/staff/moderation/reports/"+created.ReportID, nil)
	r.Header.Set("Authorization", "Bearer staff-token")
	r.Header.Set("X-Staff-Role", "admin")
	rec = staffServe(t, sh, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer status = %d body=%s", rec.Code, rec.Body.String())
	}

	raw, err := json.Marshal(map[string]any{"status": "triaged", "staffNote": "from principal"})
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest(http.MethodPost, "/v1/staff/moderation/reports/"+created.ReportID+"/status", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer staff-token")
	rec = staffServe(t, sh, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("triage status = %d body=%s", rec.Code, rec.Body.String())
	}
	var triaged staffReportDTO
	decode(t, rec, &triaged)
	if triaged.StatusChangedBy == nil || *triaged.StatusChangedBy != staffID.String() {
		t.Fatalf("actor = %+v", triaged.StatusChangedBy)
	}
}

type staticStaffProvider struct {
	principal staffauth.Principal
}

func (s staticStaffProvider) Verify(_ context.Context, cred staffauth.Credential) (staffauth.Principal, error) {
	if cred.Token != "staff-token" {
		return staffauth.Principal{}, staffauth.ErrUnauthenticated
	}
	return s.principal, nil
}

func staffServe(t *testing.T, h *StaffHandler, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}

func staffDo(t *testing.T, h *StaffHandler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	h.Register(mux)
	mux.ServeHTTP(rec, r)
	return rec
}
