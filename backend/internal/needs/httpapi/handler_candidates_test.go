package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"backend/internal/businesses"
	mdcontracts "backend/internal/masterdata/contracts"
	"backend/internal/needs"
)

func TestCandidatesOpenNeedReturnsEligible(t *testing.T) {
	h := newMatchingHandler(t)
	need := openNeedOK(t, h)
	biz := createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.624, Longitude: 29.116}, "Eligible")
	rec := do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/candidates", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto candidatesDTO
	decode(t, rec, &dto)
	if len(dto.Candidates) != 1 || dto.Candidates[0].ServiceID != biz.ID.String() {
		t.Fatalf("dto = %+v", dto)
	}
	assertNoRequesterLeak(t, rec.Body.String(), h.sessions.userID.String())
	assertNoOwnerLeakCandidates(t, rec.Body.String(), h.bizOwner.String())
}

func TestCandidatesRejectsDraftTerminalForeignAndRankingQuery(t *testing.T) {
	h := newMatchingHandler(t)
	draft := createOK(t, h.testHandler)
	rec := do(t, h, http.MethodGet, "/v1/needs/"+draft.ID.String()+"/candidates", "", nil, authedCookies())
	if rec.Code != http.StatusConflict {
		t.Fatalf("draft status = %d", rec.Code)
	}
	opened := openNeedOK(t, h)
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.svc.Fulfill(context.Background(), h.sessions.userID, opened.ID); err != nil {
		t.Fatal(err)
	}
	rec = do(t, h, http.MethodGet, "/v1/needs/"+opened.ID.String()+"/candidates", "", nil, authedCookies())
	if rec.Code != http.StatusConflict {
		t.Fatalf("terminal status = %d", rec.Code)
	}
	h.sessions.userID = mustID(t)
	rec = do(t, h, http.MethodGet, "/v1/needs/"+draft.ID.String()+"/candidates", "", nil, authedCookies())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "not_found")

	h.sessions.userID = draft.RequesterUserID
	rec = do(t, h, http.MethodGet, "/v1/needs/"+draft.ID.String()+"/candidates?sort=distance&radiusKm=1", "", nil, authedCookies())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ranking query status = %d", rec.Code)
	}
}

func TestCandidatesDistanceRadiusOrderLimitAndCategory(t *testing.T) {
	h := newMatchingHandler(t)
	need := openNeedOK(t, h)
	near := createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.623, Longitude: 29.116}, "Near")
	farther := createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.68, Longitude: 29.116}, "Farther")
	_ = createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.85, Longitude: 29.116}, "Outside")
	paused := createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.622, Longitude: 29.116}, "Paused")
	if _, err := h.biz.PauseOfferedService(context.Background(), h.bizOwner, paused.BusinessID, paused.ID); err != nil {
		t.Fatal(err)
	}

	rec := do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/candidates", "", nil, authedCookies())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var dto candidatesDTO
	decode(t, rec, &dto)
	if len(dto.Candidates) != 2 || dto.Candidates[0].ServiceID != near.ID.String() || dto.Candidates[1].ServiceID != farther.ID.String() {
		t.Fatalf("order = %+v", dto)
	}

	rec = do(t, h, http.MethodGet, "/v1/needs/"+need.ID.String()+"/candidates?limit=1", "", nil, authedCookies())
	decode(t, rec, &dto)
	if rec.Code != http.StatusOK || len(dto.Candidates) != 1 || dto.Candidates[0].ServiceID != near.ID.String() {
		t.Fatalf("limit = %+v status=%d", dto, rec.Code)
	}

	cat := mustBizID(t)
	h.cats.ok[mdcontracts.ID(cat)] = struct{}{}
	needCatID := needs.ID(cat)
	needCatContent := needs.Content{
		Title:      "CatNeed",
		Location:   needs.Coordinates{Latitude: 36.621, Longitude: 29.116},
		CategoryID: &needCatID,
	}
	needCat, err := h.svc.Create(context.Background(), h.sessions.userID, needCatContent)
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.svc.Open(context.Background(), h.sessions.userID, needCat.ID); err != nil {
		t.Fatal(err)
	}
	matched := createLocatedActiveServiceContent(t, h, businesses.Coordinates{Latitude: 36.622, Longitude: 29.116}, businesses.ServiceContent{
		Title: "CatSvc", CategoryID: &cat,
	})
	_ = createLocatedActiveService(t, h, businesses.Coordinates{Latitude: 36.623, Longitude: 29.116}, "Uncat")
	rec = do(t, h, http.MethodGet, "/v1/needs/"+needCat.ID.String()+"/candidates", "", nil, authedCookies())
	decode(t, rec, &dto)
	if rec.Code != http.StatusOK || len(dto.Candidates) != 1 || dto.Candidates[0].ServiceID != matched.ID.String() {
		t.Fatalf("category = %+v status=%d", dto, rec.Code)
	}
}

type matchingHandler struct {
	*testHandler
	biz      *businesses.Service
	bizStore *businesses.MemoryStore
	bizOwner businesses.ID
	cats     *publishedCats
}

func newMatchingHandler(t *testing.T) *matchingHandler {
	t.Helper()
	needStore := needs.NewMemoryStore()
	bizStore := businesses.NewMemoryStore()
	clock := &frozenNow{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)}
	cats := &publishedCats{ok: map[mdcontracts.ID]struct{}{}}
	bizSvc, err := businesses.NewService(bizStore, cats, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	needSvc, err := needs.NewService(needStore, cats, bizSvc, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{userID: mustID(t)}
	h, err := New(sessions, needSvc, []string{allowedOrigin})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return &matchingHandler{
		testHandler: &testHandler{Handler: h, sessions: sessions, store: needStore, svc: needSvc, clock: clock},
		biz:         bizSvc,
		bizStore:    bizStore,
		bizOwner:    owner,
		cats:        cats,
	}
}

func openNeedOK(t *testing.T, h *matchingHandler) needs.Need {
	t.Helper()
	created := createOK(t, h.testHandler)
	h.clock.now = h.clock.now.Add(time.Minute)
	opened, err := h.svc.Open(context.Background(), h.sessions.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return opened
}

func createLocatedActiveService(t *testing.T, h *matchingHandler, loc businesses.Coordinates, title string) businesses.OfferedService {
	t.Helper()
	return createLocatedActiveServiceContent(t, h, loc, businesses.ServiceContent{Title: title})
}

func createLocatedActiveServiceContent(t *testing.T, h *matchingHandler, loc businesses.Coordinates, content businesses.ServiceContent) businesses.OfferedService {
	t.Helper()
	owner, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	h.bizOwner = owner
	profile, err := h.biz.Create(context.Background(), owner, businesses.ProfileContent{DisplayName: "Kafe"})
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.biz.UpdateLocation(context.Background(), owner, profile.ID, &loc); err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if _, err := h.biz.Activate(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	offered, err := h.biz.CreateOfferedService(context.Background(), owner, profile.ID, content)
	if err != nil {
		t.Fatal(err)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	active, err := h.biz.ActivateOfferedService(context.Background(), owner, profile.ID, offered.ID)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func assertNoOwnerLeakCandidates(t *testing.T, body, ownerID string) {
	t.Helper()
	if strings.Contains(strings.ToLower(body), "owneruserid") || strings.Contains(body, ownerID) {
		t.Fatalf("owner leaked in %q", body)
	}
}

func mustBizID(t *testing.T) businesses.ID {
	t.Helper()
	id, err := businesses.NewID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type publishedCats struct {
	ok map[mdcontracts.ID]struct{}
}

func (s *publishedCats) RequirePublished(ctx context.Context, categoryID mdcontracts.ID) error {
	if categoryID.IsZero() {
		return mdcontracts.ErrZeroID
	}
	if _, ok := s.ok[categoryID]; !ok {
		return mdcontracts.ErrNotFound
	}
	return nil
}
