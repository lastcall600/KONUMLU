package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"backend/internal/businesses"
)

func TestCreateServiceRequiresAuthOriginCSRF(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	path := "/v1/businesses/" + biz.ID.String() + "/services"

	rec := do(t, h, http.MethodPost, path, allowedOrigin, validServiceBody(), map[string]string{
		csrfCookieName: "csrf-token",
	}, withCSRF("csrf-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, path, "", validServiceBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing origin status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, validServiceBody(), authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", rec.Code)
	}
}

func TestOwnerCreatesService(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	rec := do(t, h, http.MethodPost, "/v1/businesses/"+biz.ID.String()+"/services", allowedOrigin, validServiceBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerServiceDTO
	decode(t, rec, &dto)
	if dto.Status != string(businesses.ServiceStatusDraft) || dto.Title != "Tur Кафе" || dto.BusinessID != biz.ID.String() {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Description == nil || *dto.Description != "Açıklama" {
		t.Fatalf("description = %v", dto.Description)
	}
	assertNoOwnerLeak(t, rec.Body.String(), h.sessions.userID.String())
}

func TestUnrelatedUserDeniedService(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	created := createServiceOK(t, h, biz.ID)
	h.sessions.userID = mustID(t)
	rec := do(t, h, http.MethodGet, "/v1/businesses/"+biz.ID.String()+"/services/"+created.ID.String(), "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")
	if strings.Contains(rec.Body.String(), created.Title) {
		t.Fatal("must not leak title")
	}
	rec = do(t, h, http.MethodPost, "/v1/businesses/"+biz.ID.String()+"/services", allowedOrigin, validServiceBody(), authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("create status = %d", rec.Code)
	}
}

func TestCreateServiceRejectsSpoofedFields(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	body := validServiceBody()
	body["status"] = "active"
	body["ownerUserId"] = mustID(t).String()
	body["businessId"] = mustID(t).String()
	body["serviceId"] = mustID(t).String()
	rec := do(t, h, http.MethodPost, "/v1/businesses/"+biz.ID.String()+"/services", allowedOrigin, body, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	list, err := h.svc.ListOwnedOfferedServices(context.Background(), biz.OwnerUserID, biz.ID)
	if err != nil || len(list) != 0 {
		t.Fatalf("must not create list=%+v err=%v", list, err)
	}
}

func TestServiceLifecycleHTTP(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	created := createServiceOK(t, h, biz.ID)
	base := "/v1/businesses/" + biz.ID.String() + "/services/" + created.ID.String()

	rec := do(t, h, http.MethodPost, base+"/pause", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("pause draft status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, base+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("activate status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerServiceDTO
	decode(t, rec, &dto)
	if dto.Status != string(businesses.ServiceStatusActive) {
		t.Fatalf("dto = %+v", dto)
	}

	rec = do(t, h, http.MethodPost, base+"/pause", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("pause status = %d", rec.Code)
	}
	decode(t, rec, &dto)
	if dto.Status != string(businesses.ServiceStatusPaused) {
		t.Fatalf("dto = %+v", dto)
	}

	rec = do(t, h, http.MethodPost, base+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("reactivate status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, base+"/close", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("close status = %d", rec.Code)
	}
	decode(t, rec, &dto)
	if dto.Status != string(businesses.ServiceStatusClosed) {
		t.Fatalf("dto = %+v", dto)
	}

	rec = do(t, h, http.MethodPost, base+"/activate", allowedOrigin, map[string]any{}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("reopen status = %d", rec.Code)
	}
}

func TestServicePriceValidationHTTP(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	path := "/v1/businesses/" + biz.ID.String() + "/services"

	rec := do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"title": "Tur", "priceModel": "fixed", "amount": "100.00",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fixed without currency status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"title": "Tur", "priceModel": "quote_required", "amount": "10", "currency": "TRY",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("quote with amount status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodPost, path, allowedOrigin, map[string]any{
		"title": "Tur", "priceModel": "fixed", "amount": "120.50", "currency": "TRY",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusOK {
		t.Fatalf("fixed status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto ownerServiceDTO
	decode(t, rec, &dto)
	if dto.PriceModel == nil || *dto.PriceModel != "fixed" || dto.Amount == nil || *dto.Amount != "120.50" || dto.Currency == nil || *dto.Currency != "TRY" {
		t.Fatalf("dto = %+v", dto)
	}
}

func TestPublicServiceVisibilityAndPrivacy(t *testing.T) {
	h := newTestHandler(t)
	draftBiz := createOK(t, h)
	draftSvc := createServiceOK(t, h, draftBiz.ID)
	if _, err := h.svc.ActivateOfferedService(context.Background(), draftBiz.OwnerUserID, draftBiz.ID, draftSvc.ID); err != nil {
		t.Fatal(err)
	}

	activeBiz := createOKFor(t, h, mustID(t))
	if _, err := h.svc.Activate(context.Background(), activeBiz.OwnerUserID, activeBiz.ID); err != nil {
		t.Fatal(err)
	}
	paused := createServiceOKFor(t, h, activeBiz)
	if _, err := h.svc.ActivateOfferedService(context.Background(), activeBiz.OwnerUserID, activeBiz.ID, paused.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.PauseOfferedService(context.Background(), activeBiz.OwnerUserID, activeBiz.ID, paused.ID); err != nil {
		t.Fatal(err)
	}
	closed := createServiceOKFor(t, h, activeBiz)
	if _, err := h.svc.CloseOfferedService(context.Background(), activeBiz.OwnerUserID, activeBiz.ID, closed.ID); err != nil {
		t.Fatal(err)
	}
	draftUnderActive := createServiceOKFor(t, h, activeBiz)
	activeSvc := createServiceOKFor(t, h, activeBiz)
	if _, err := h.svc.ActivateOfferedService(context.Background(), activeBiz.OwnerUserID, activeBiz.ID, activeSvc.ID); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodGet, "/v1/public/services/"+draftSvc.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("active service under draft business status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+draftBiz.ID.String()+"/services", "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("draft business list status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/services/"+paused.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("paused public status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/services/"+closed.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("closed public status = %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/v1/public/services/"+draftUnderActive.ID.String(), "", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("draft public status = %d", rec.Code)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/services/"+activeSvc.ID.String(), "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("active public status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	decode(t, rec, &raw)
	for _, banned := range []string{"ownerUserId", "owner_user_id", "userId", "status", "moderationState", "verificationStatus"} {
		if _, ok := raw[banned]; ok {
			t.Fatalf("public DTO leaked %s: %v", banned, raw)
		}
	}
	assertNoOwnerLeak(t, rec.Body.String(), activeBiz.OwnerUserID.String())

	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+activeBiz.ID.String()+"/services", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list publicServiceListDTO
	decode(t, rec, &list)
	if len(list.Items) != 1 || list.Items[0].ServiceID != activeSvc.ID.String() {
		t.Fatalf("public list = %+v", list)
	}
}

func TestOwnerAndPublicListsDeterministic(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	if _, err := h.svc.Activate(context.Background(), biz.OwnerUserID, biz.ID); err != nil {
		t.Fatal(err)
	}
	first := createServiceOK(t, h, biz.ID)
	second := createServiceOK(t, h, biz.ID)
	if _, err := h.svc.ActivateOfferedService(context.Background(), biz.OwnerUserID, biz.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ActivateOfferedService(context.Background(), biz.OwnerUserID, biz.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := h.store.ListOfferedServices(context.Background(), biz.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}

	rec := do(t, h, http.MethodGet, "/v1/businesses/"+biz.ID.String()+"/services", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("owner list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var ownerList serviceListDTO
	decode(t, rec, &ownerList)
	if len(ownerList.Items) != 2 || ownerList.Items[0].ServiceID != stored[0].ID.String() || ownerList.Items[1].ServiceID != stored[1].ID.String() {
		t.Fatalf("owner list = %+v stored=%+v", ownerList, stored)
	}

	rec = do(t, h, http.MethodGet, "/v1/public/businesses/"+biz.ID.String()+"/services", "", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("public list status = %d", rec.Code)
	}
	var pub publicServiceListDTO
	decode(t, rec, &pub)
	if len(pub.Items) != 2 || pub.Items[0].ServiceID != stored[0].ID.String() || pub.Items[1].ServiceID != stored[1].ID.String() {
		t.Fatalf("public list = %+v", pub)
	}
}

func TestActivateServiceRejectsClientStatus(t *testing.T) {
	h := newTestHandler(t)
	biz := createOK(t, h)
	created := createServiceOK(t, h, biz.ID)
	rec := do(t, h, http.MethodPost, "/v1/businesses/"+biz.ID.String()+"/services/"+created.ID.String()+"/activate", allowedOrigin, map[string]any{
		"status": "closed",
	}, authedCookies(), withCSRF("csrf-token"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func validServiceBody() map[string]any {
	return map[string]any{
		"title":       "Tur Кафе",
		"description": "Açıklama",
	}
}

func createServiceOK(t *testing.T, h *testHandler, businessID businesses.ID) businesses.OfferedService {
	t.Helper()
	svc, err := h.svc.CreateOfferedService(context.Background(), h.sessions.userID, businessID, businesses.ServiceContent{
		Title:       "Tur Кафе",
		Description: "Açıklama",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func createServiceOKFor(t *testing.T, h *testHandler, biz businesses.Profile) businesses.OfferedService {
	t.Helper()
	svc, err := h.svc.CreateOfferedService(context.Background(), biz.OwnerUserID, biz.ID, businesses.ServiceContent{
		Title:       "Tur Кафе",
		Description: "Açıklama",
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
