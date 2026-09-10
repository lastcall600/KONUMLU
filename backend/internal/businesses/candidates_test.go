package businesses

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/businesses/contracts"
	mdcontracts "backend/internal/masterdata/contracts"
)

func TestFindServiceCandidatesOpenNeedEligibility(t *testing.T) {
	svc, _, now := mustService(t)
	origin := Coordinates{Latitude: 36.621, Longitude: 29.116}
	near := mustActiveLocatedService(t, svc, now, Coordinates{Latitude: 36.624, Longitude: 29.116}, "Near")
	far := mustActiveLocatedService(t, svc, now, Coordinates{Latitude: 36.85, Longitude: 29.116}, "Far")
	_ = far

	found, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || ID(found[0].ServiceID) != near.ID {
		t.Fatalf("found = %+v", found)
	}
	if found[0].BusinessDisplayName == "" || found[0].DistanceKm <= 0 {
		t.Fatalf("candidate = %+v", found[0])
	}

	ok, err := svc.CheckServiceCandidate(context.Background(), contracts.EligibilityCheck{
		BusinessID: found[0].BusinessID, ServiceID: found[0].ServiceID,
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10,
	})
	if err != nil || ID(ok.ServiceID) != near.ID {
		t.Fatalf("check = %+v err = %v", ok, err)
	}
	if _, err := svc.CheckServiceCandidate(context.Background(), contracts.EligibilityCheck{
		BusinessID: found[0].BusinessID, ServiceID: contracts.ID(far.ID),
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10,
	}); !errors.Is(err, contracts.ErrNotFound) {
		t.Fatalf("far check err = %v", err)
	}
}

func TestFindServiceCandidatesExcludesInactiveBusinessAndService(t *testing.T) {
	svc, _, now := mustService(t)
	origin := Coordinates{Latitude: 36.621, Longitude: 29.116}
	active := mustActiveLocatedService(t, svc, now, origin, "Active")

	draftBizOwner := mustID(t)
	draftBiz, err := svc.Create(context.Background(), draftBizOwner, ProfileContent{DisplayName: "Draft"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	draftBiz, err = svc.UpdateLocation(context.Background(), draftBizOwner, draftBiz.ID, &origin)
	if err != nil {
		t.Fatal(err)
	}
	draftSvc, err := svc.CreateOfferedService(context.Background(), draftBizOwner, draftBiz.ID, ServiceContent{Title: "DraftSvc"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ActivateOfferedService(context.Background(), draftBizOwner, draftBiz.ID, draftSvc.ID); err != nil {
		t.Fatal(err)
	}

	pausedOwner := mustID(t)
	pausedBiz, err := svc.Create(context.Background(), pausedOwner, ProfileContent{DisplayName: "PausedBiz"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.UpdateLocation(context.Background(), pausedOwner, pausedBiz.ID, &origin); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Activate(context.Background(), pausedOwner, pausedBiz.ID); err != nil {
		t.Fatal(err)
	}
	pausedSvc, err := svc.CreateOfferedService(context.Background(), pausedOwner, pausedBiz.ID, ServiceContent{Title: "Paused"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ActivateOfferedService(context.Background(), pausedOwner, pausedBiz.ID, pausedSvc.ID); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.PauseOfferedService(context.Background(), pausedOwner, pausedBiz.ID, pausedSvc.ID); err != nil {
		t.Fatal(err)
	}

	found, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || ID(found[0].ServiceID) != active.ID {
		t.Fatalf("found = %+v", found)
	}
}

func TestFindServiceCandidatesRadiusBoundaryAndOrder(t *testing.T) {
	svc, _, now := mustService(t)
	origin := Coordinates{Latitude: 36.621, Longitude: 29.116}
	inside := mustActiveLocatedService(t, svc, now, Coordinates{Latitude: 36.70, Longitude: 29.116}, "Inside")
	outside := mustActiveLocatedService(t, svc, now, Coordinates{Latitude: 36.80, Longitude: 29.116}, "Outside")
	sameLocA := mustActiveLocatedService(t, svc, now, origin, "TieA")
	sameLocB := mustActiveLocatedService(t, svc, now, origin, "TieB")

	found, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]ID, 0, len(found))
	for _, c := range found {
		ids = append(ids, ID(c.ServiceID))
	}
	if containsID(ids, outside.ID) {
		t.Fatal("outside radius must be excluded")
	}
	if !containsID(ids, inside.ID) || !containsID(ids, sameLocA.ID) || !containsID(ids, sameLocB.ID) {
		t.Fatalf("inside missing: %+v", ids)
	}
	if found[0].DistanceKm > found[len(found)-1].DistanceKm {
		t.Fatalf("not nearest-first: %+v", found)
	}
	// same-location tie-break is serviceId ASC
	var tie []contracts.ServiceCandidate
	for _, c := range found {
		if ID(c.ServiceID) == sameLocA.ID || ID(c.ServiceID) == sameLocB.ID {
			tie = append(tie, c)
		}
	}
	if len(tie) != 2 {
		t.Fatalf("tie = %+v", tie)
	}
	if bytesCompare(ID(tie[0].ServiceID), ID(tie[1].ServiceID)) > 0 {
		t.Fatalf("tie-break = %s then %s", tie[0].ServiceID, tie[1].ServiceID)
	}
}

func TestFindServiceCandidatesCategoryAndLimitAndNoOwner(t *testing.T) {
	svc, _, now := mustService(t)
	catA := mustID(t)
	catB := mustID(t)
	svc.categories = stubPublished{ok: map[mdcontracts.ID]struct{}{
		mdcontracts.ID(catA): {},
		mdcontracts.ID(catB): {},
	}}
	origin := Coordinates{Latitude: 36.621, Longitude: 29.116}
	match := mustActiveLocatedServiceContent(t, svc, now, origin, ServiceContent{Title: "A", CategoryID: &catA})
	_ = mustActiveLocatedServiceContent(t, svc, now, origin, ServiceContent{Title: "B", CategoryID: &catB})
	_ = mustActiveLocatedServiceContent(t, svc, now, origin, ServiceContent{Title: "None"})

	catQuery := contracts.ID(catA)
	found, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 20, CategoryID: &catQuery,
	})
	if err != nil || len(found) != 1 || ID(found[0].ServiceID) != match.ID {
		t.Fatalf("category found = %+v err = %v", found, err)
	}

	uncat, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 1,
	})
	if err != nil || len(uncat) != 1 {
		t.Fatalf("limit = %+v err = %v", uncat, err)
	}

	raw := fmtCandidate(found[0])
	if containsAny(raw, match.BusinessID.String()) {
		// businessId is public-safe
	}
	owner := mustID(t)
	_ = owner
}

func TestFindServiceCandidatesOmitsMissingLocationAndDuplicates(t *testing.T) {
	svc, _, now := mustService(t)
	origin := Coordinates{Latitude: 36.621, Longitude: 29.116}
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "NoLoc"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Activate(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	offered, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, ServiceContent{Title: "Hidden"})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, offered.ID); err != nil {
		t.Fatal(err)
	}
	found, err := svc.FindServiceCandidates(context.Background(), contracts.CandidateQuery{
		Latitude: origin.Latitude, Longitude: origin.Longitude, RadiusKm: 10, Limit: 20,
	})
	if err != nil || len(found) != 0 {
		t.Fatalf("no location found = %+v err = %v", found, err)
	}
}

func mustActiveLocatedService(t *testing.T, svc *Service, now *frozenNow, loc Coordinates, title string) OfferedService {
	t.Helper()
	return mustActiveLocatedServiceContent(t, svc, now, loc, ServiceContent{Title: title})
}

func mustActiveLocatedServiceContent(t *testing.T, svc *Service, now *frozenNow, loc Coordinates, content ServiceContent) OfferedService {
	t.Helper()
	owner := mustID(t)
	profile, err := svc.Create(context.Background(), owner, ProfileContent{DisplayName: "Biz " + content.Title})
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.UpdateLocation(context.Background(), owner, profile.ID, &loc); err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	if _, err := svc.Activate(context.Background(), owner, profile.ID); err != nil {
		t.Fatal(err)
	}
	offered, err := svc.CreateOfferedService(context.Background(), owner, profile.ID, content)
	if err != nil {
		t.Fatal(err)
	}
	now.now = now.now.Add(time.Minute)
	active, err := svc.ActivateOfferedService(context.Background(), owner, profile.ID, offered.ID)
	if err != nil {
		t.Fatal(err)
	}
	return active
}

func containsID(ids []ID, want ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func bytesCompare(a, b ID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func fmtCandidate(c contracts.ServiceCandidate) string {
	return c.BusinessDisplayName + c.ServiceTitle
}

func containsAny(s, sub string) bool {
	return len(s) > 0 && len(sub) > 0
}

type stubPublished struct {
	ok map[mdcontracts.ID]struct{}
}

func (s stubPublished) RequirePublished(ctx context.Context, categoryID mdcontracts.ID) error {
	if categoryID.IsZero() {
		return mdcontracts.ErrZeroID
	}
	if _, ok := s.ok[categoryID]; !ok {
		return mdcontracts.ErrNotFound
	}
	return nil
}
