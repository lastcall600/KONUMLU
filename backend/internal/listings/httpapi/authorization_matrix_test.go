package httpapi

import (
	"net/http"
	"testing"
	"time"

	"backend/internal/listings"
)

func TestOwnerNonOwnerListingAuthorizationMatrix(t *testing.T) {
	h := newTestHandler(t)
	created := createDraft(t, h)
	id := created.ID.String()
	owner := h.sessions.userID

	rec := do(t, h, http.MethodGet, "/v1/listings/"+id, "", nil, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET status = %d", rec.Code)
	}
	assertErrorCode(t, rec, "unauthenticated")

	rec = do(t, h, http.MethodGet, "/v1/listings/"+id, "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("owner GET status = %d body=%s", rec.Code, rec.Body.String())
	}

	patchBody := map[string]any{
		"title":     "Updated bike",
		"updatedAt": created.UpdatedAt.UTC().Format(time.RFC3339),
	}
	rec = do(t, h, http.MethodPatch, "/v1/listings/"+id, allowedOrigin, patchBody, authedCookies())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner PATCH missing CSRF status = %d", rec.Code)
	}

	h.sessions.userID = mustID(t)
	foreign := authedCookies()
	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/v1/listings/" + id, nil},
		{http.MethodPatch, "/v1/listings/" + id, patchBody},
		{http.MethodPost, "/v1/listings/" + id + "/archive", nil},
		{http.MethodPost, "/v1/listings/" + id + "/publish", nil},
		{http.MethodPost, "/v1/listings/" + id + "/ready", nil},
	}
	for _, c := range cases {
		rec = do(t, h, c.method, c.path, allowedOrigin, c.body, foreign, withCSRF("csrf-token"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("non-owner %s %s status = %d body=%s", c.method, c.path, rec.Code, rec.Body.String())
		}
		assertErrorCode(t, rec, "not_found")
		assertNoSensitiveLeak(t, rec.Body.String())
	}

	stored, err := h.listingStore.Get(t.Context(), created.ID)
	if err != nil || stored.Status != listings.StatusDraft || stored.OwnerUserID != owner {
		t.Fatalf("must not mutate foreign listing: %+v err=%v", stored, err)
	}
}
