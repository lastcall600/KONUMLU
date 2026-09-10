package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"backend/internal/moderation"
)

func TestConsumerAppealEligibleSubmitListReadWithdraw(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.sessions.userID)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)

	rec := do(t, h, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, map[string]any{
		"actionId":  action.ID.String(),
		"statement": "this is my listing",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status = %d body=%s", rec.Code, rec.Body.String())
	}
	rawBytes := rec.Body.Bytes()
	var created appealDTO
	decode(t, rec, &created)
	if created.Status != "submitted" || created.ActionID != action.ID.String() {
		t.Fatalf("created = %+v", created)
	}
	assertAppealDTOPrivacy(t, rawBytes)

	rec = do(t, h, http.MethodGet, "/v1/moderation/appeals/mine", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("mine status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAppealDTOPrivacy(t, rec.Body.Bytes())
	var list appealListDTO
	decode(t, rec, &list)
	if len(list.Appeals) != 1 || list.Appeals[0].AppealID != created.AppealID {
		t.Fatalf("mine = %+v", list)
	}

	rec = do(t, h, http.MethodGet, "/v1/moderation/appeals/"+created.AppealID, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = do(t, h, http.MethodPost, "/v1/moderation/appeals/"+created.AppealID+"/withdraw", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d body=%s", rec.Code, rec.Body.String())
	}
	var withdrawn appealDTO
	decode(t, rec, &withdrawn)
	if withdrawn.Status != "withdrawn" || withdrawn.DecidedAt == nil {
		t.Fatalf("withdrawn = %+v", withdrawn)
	}
}

func TestConsumerAppealUnrelatedDeniedPrivacySafe(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.other)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)
	rec := do(t, h, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, map[string]any{
		"actionId":  action.ID.String(),
		"statement": "not mine",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated submit status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")

	ownerAppeal, err := h.svc.SubmitAppeal(t.Context(), h.other, moderation.CreateAppealInput{
		ActionID: action.ID, Statement: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/moderation/appeals/"+ownerAppeal.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated get status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/appeals/"+ownerAppeal.ID.String()+"/withdraw", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unrelated withdraw status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConsumerAppealRejectsAppellantSpoofAndStaffFields(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.sessions.userID)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)
	rec := do(t, h, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, map[string]any{
		"actionId":        action.ID.String(),
		"statement":       "spoof",
		"appellantUserId": mustID(t).String(),
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("spoof status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestConsumerAppealDuplicateAndWindow(t *testing.T) {
	h := newTestHandler(t)
	listing := h.publishListing(t, h.sessions.userID)
	action := h.mustExecutedAction(t, moderation.TargetListing, listing)
	body := map[string]any{"actionId": action.ID.String(), "statement": "once"}
	rec := do(t, h, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body=%s", rec.Code, rec.Body.String())
	}

	h2 := newTestHandler(t)
	listing2 := h2.publishListing(t, h2.sessions.userID)
	action2 := h2.mustExecutedAction(t, moderation.TargetListing, listing2)
	h2.clock.now = action2.UpdatedAt.Add(moderation.DefaultAppealWindow + time.Second)
	rec = do(t, h2, http.MethodPost, "/v1/moderation/appeals", allowedOrigin, map[string]any{
		"actionId":  action2.ID.String(),
		"statement": "late",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("window status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func (h *testHandler) mustExecutedAction(t *testing.T, targetType moderation.TargetType, subject moderation.ID) moderation.CaseAction {
	t.Helper()
	detail, err := h.svc.CreateCase(t.Context(), moderation.CreateCaseInput{
		SubjectType: targetType, SubjectID: subject, Title: "http appeal",
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := h.svc.CreateAction(t.Context(), detail.Case.ID, moderation.CreateActionInput{
		TargetType: targetType, TargetID: subject, ActionType: moderation.ActionTypeWarning,
		ReasonCode: moderation.ActionReasonPolicyViolation,
	})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Second)
	if _, err := h.svc.TransitionAction(t.Context(), detail.Case.ID, row.ID, moderation.ActionTransitionInput{To: moderation.ActionStatusApproved}); err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Second)
	executed, err := h.svc.TransitionAction(t.Context(), detail.Case.ID, row.ID, moderation.ActionTransitionInput{To: moderation.ActionStatusExecuted})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Second)
	return executed
}

func assertAppealDTOPrivacy(t *testing.T, raw []byte) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{}
	if _, ok := payload["appealId"]; ok {
		rows = append(rows, payload)
	}
	if list, ok := payload["appeals"].([]any); ok {
		for _, item := range list {
			row, ok := item.(map[string]any)
			if !ok {
				t.Fatalf("appeal row = %#v", item)
			}
			rows = append(rows, row)
		}
	}
	if len(rows) == 0 {
		t.Fatalf("payload = %#v", payload)
	}
	for _, row := range rows {
		for _, key := range []string{
			"appellantUserId", "appellant_user_id", "decidedByStaffId", "decided_by_staff_id",
			"staffNote", "staff_note", "reporterUserId", "reporter_user_id", "actorStaffId",
			"evidence", "note",
		} {
			if _, present := row[key]; present {
				t.Fatalf("leaked %s: %#v", key, row)
			}
		}
	}
}
