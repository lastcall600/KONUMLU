package publicprofile

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/identity"
	"backend/internal/identity/contracts"
	staffauth "backend/internal/staffauth/contracts"
)

func TestStaffProfileGet(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	staff, err := NewStaff(allowStaff{}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, staff, "/v1/staff/identity/profiles/"+id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body staffProfileDTO
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.PublicProfileID != id {
		t.Fatalf("id = %s", body.PublicProfileID)
	}
	if body.ModerationState != string(ModerationNone) {
		t.Fatalf("moderation = %s", body.ModerationState)
	}
	if !body.AccountEligible || body.Disabled || body.Deleted {
		t.Fatalf("account = %+v", body)
	}
	assertPrivacy(t, rec.Body.String(), h.user.ID)
}

func TestStaffProfileRestrictedStillReadable(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	publicID, err := ParsePublicID(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.ApplyModerationState(t.Context(), mustApplyRestricted(t, publicID)); err != nil {
		t.Fatal(err)
	}
	pub := do(t, h, http.MethodGet, "/v1/public/profiles/"+id, "", nil, nil)
	if pub.Code != http.StatusNotFound {
		t.Fatalf("public status = %d", pub.Code)
	}
	staff, err := NewStaff(allowStaff{}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, staff, "/v1/staff/identity/profiles/"+id)
	if rec.Code != http.StatusOK {
		t.Fatalf("staff status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"moderationState":"restricted"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestStaffProfileAuthZ(t *testing.T) {
	h := newHTTPFixture(t)
	unauth, err := NewStaff(stubStaff{err: staffauth.ErrUnauthenticated}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, unauth, "/v1/staff/identity/profiles/"+mustID(t).String())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d", rec.Code)
	}
	forbidden, err := NewStaff(stubStaff{deny: map[staffauth.Permission]bool{staffauth.PermIdentityProfileRead: true}}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, forbidden, "/v1/staff/identity/profiles/"+mustID(t).String())
	if rec.Code != http.StatusForbidden {
		t.Fatalf("forbidden = %d", rec.Code)
	}
	staff, err := NewStaff(allowStaff{}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec = staffDo(t, staff, "/v1/staff/identity/profiles/"+mustID(t).String())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown = %d body=%s", rec.Code, rec.Body.String())
	}
	rec = staffDo(t, staff, "/v1/staff/identity/profiles/not-a-uuid")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad id = %d", rec.Code)
	}
}

func TestStaffProfileDisabledAccount(t *testing.T) {
	h := newHTTPFixture(t)
	me := do(t, h, http.MethodGet, "/v1/profile/me", "", nil, authedCookies())
	id := decodeDTO(t, me).PublicProfileID
	at := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	user := h.user
	user.DisabledAt = &at
	h.store.PutUser(user)
	staff, err := NewStaff(allowStaff{}, h.svc)
	if err != nil {
		t.Fatal(err)
	}
	rec := staffDo(t, staff, "/v1/staff/identity/profiles/"+id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body staffProfileDTO
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.AccountEligible || !body.Disabled {
		t.Fatalf("account = %+v", body)
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

func staffDo(t *testing.T, h *StaffHandler, path string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func mustApplyRestricted(t *testing.T, publicID identity.ID) contracts.ApplyPublicProfileModerationInput {
	t.Helper()
	var id contracts.ID
	copy(id[:], publicID[:])
	return contracts.ApplyPublicProfileModerationInput{PublicProfileID: id, State: "restricted"}
}
