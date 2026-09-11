package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identitycontracts "backend/internal/identity/contracts"
	"backend/internal/listings"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffListingGetAndOwnerList(t *testing.T) {
	env := newTestHandler(t)
	listing := createDraft(t, env)
	publicID := mustID(t)
	profiles := &staffProfiles{
		byUser: map[identitycontracts.ID]identitycontracts.StaffProfile{
			identitycontracts.ID(env.sessions.userID): {
				PublicProfileID: identitycontracts.ID(publicID),
				DisplayName:     strPtr("Ada"),
			},
		},
		byPublic: map[identitycontracts.ID]identitycontracts.ID{
			identitycontracts.ID(publicID): identitycontracts.ID(env.sessions.userID),
		},
	}
	staff, err := NewStaff(allowStaff{}, env.svc, env.geo, env.publicMedia, profiles)
	if err != nil {
		t.Fatal(err)
	}

	rec := staffDo(t, staff, http.MethodGet, "/v1/staff/listings/"+listing.ID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto staffListingDTO
	decode(t, rec, &dto)
	if dto.ListingID != listing.ID.String() || dto.Title != "Bike" || dto.Status != string(listings.StatusDraft) {
		t.Fatalf("dto = %+v", dto)
	}
	if dto.Owner == nil || dto.Owner.PublicProfileID != publicID.String() {
		t.Fatalf("owner = %+v", dto.Owner)
	}
	if strings.Contains(rec.Body.String(), env.sessions.userID.String()) {
		t.Fatalf("owner user id leaked: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"url"`) {
		t.Fatalf("media url leaked: %s", rec.Body.String())
	}

	rec = staffDo(t, staff, http.MethodGet, "/v1/staff/listings?ownerPublicProfileId="+publicID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var page staffListingListDTO
	decode(t, rec, &page)
	if len(page.Listings) != 1 || page.Listings[0].ListingID != listing.ID.String() {
		t.Fatalf("page = %+v", page)
	}
}

func TestStaffListingAuthZAndLookup(t *testing.T) {
	env := newTestHandler(t)
	listing := createDraft(t, env)
	unauth, err := NewStaff(stubStaff{err: staffauth.ErrUnauthenticated}, env.svc, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, unauth, http.MethodGet, "/v1/staff/listings/"+listing.ID.String())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d", rec.Code)
	}
	deny, err := NewStaff(stubStaff{deny: map[staffauth.Permission]bool{staffauth.PermListingsRead: true}}, env.svc, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, deny, http.MethodGet, "/v1/staff/listings/"+listing.ID.String())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden = %d", rec.Code)
	}
	staff, err := NewStaff(allowStaff{}, env.svc, nil, nil, &staffProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, staff, http.MethodGet, "/v1/staff/listings/"+mustID(t).String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d", rec.Code)
	}
	rec = staffDo(t, staff, http.MethodGet, "/v1/staff/listings")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing owner = %d", rec.Code)
	}
	rec = staffDo(t, staff, http.MethodGet, "/v1/staff/listings?ownerPublicProfileId="+mustID(t).String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown owner = %d", rec.Code)
	}
}

type allowStaff struct{}

func (allowStaff) Authenticate(*http.Request) (staffauth.Principal, error) {
	var id staffauth.ID
	id[0] = 1
	return staffauth.Principal{StaffID: id, Roles: []staffauth.Role{staffauth.RoleModerator}}, nil
}

func (allowStaff) Allows(staffauth.Principal, staffauth.Permission) bool { return true }

type stubStaff struct {
	err  error
	deny map[staffauth.Permission]bool
}

func (s stubStaff) Authenticate(*http.Request) (staffauth.Principal, error) {
	if s.err != nil {
		return staffauth.Principal{}, s.err
	}
	var id staffauth.ID
	id[0] = 1
	return staffauth.Principal{StaffID: id}, nil
}

func (s stubStaff) Allows(_ staffauth.Principal, perm staffauth.Permission) bool {
	return !s.deny[perm]
}

type staffProfiles struct {
	byUser   map[identitycontracts.ID]identitycontracts.StaffProfile
	byPublic map[identitycontracts.ID]identitycontracts.ID
}

func (s *staffProfiles) StaffByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.StaffProfile, error) {
	return identitycontracts.StaffProfile{}, identitycontracts.ErrUnavailable
}

func (s *staffProfiles) StaffByUserID(_ context.Context, userID identitycontracts.ID) (identitycontracts.StaffProfile, error) {
	if s == nil || s.byUser == nil {
		return identitycontracts.StaffProfile{}, identitycontracts.ErrNotFound
	}
	got, ok := s.byUser[userID]
	if !ok {
		return identitycontracts.StaffProfile{}, identitycontracts.ErrNotFound
	}
	return got, nil
}

func (s *staffProfiles) StaffUserIDByPublicID(_ context.Context, publicProfileID identitycontracts.ID) (identitycontracts.ID, error) {
	if s == nil || s.byPublic == nil {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	got, ok := s.byPublic[publicProfileID]
	if !ok {
		return identitycontracts.ID{}, identitycontracts.ErrNotFound
	}
	return got, nil
}

func staffDo(t *testing.T, h *StaffHandler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func strPtr(v string) *string { return &v }
