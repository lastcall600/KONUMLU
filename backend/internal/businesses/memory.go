package businesses

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"backend/internal/businesses/contracts"
)

type MemoryStore struct {
	mu       sync.Mutex
	byID     map[ID]Profile
	byOwner  map[ID]ID
	services map[ID]OfferedService
	fail     error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Profile), byOwner: make(map[ID]ID), services: make(map[ID]OfferedService)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, profile Profile) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[profile.ID]; ok {
		return errConflict
	}
	if _, ok := m.byOwner[profile.OwnerUserID]; ok {
		return errConflict
	}
	m.byID[profile.ID] = cloneProfile(profile)
	m.byOwner[profile.OwnerUserID] = profile.ID
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Profile, error) {
	if m == nil {
		return Profile{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Profile{}, m.fail
	}
	profile, ok := m.byID[id]
	if !ok {
		return Profile{}, errNotFound
	}
	return cloneProfile(profile), nil
}

func (m *MemoryStore) GetByOwner(ctx context.Context, ownerUserID ID) (Profile, error) {
	if m == nil {
		return Profile{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Profile{}, m.fail
	}
	id, ok := m.byOwner[ownerUserID]
	if !ok {
		return Profile{}, errNotFound
	}
	profile, ok := m.byID[id]
	if !ok {
		return Profile{}, errNotFound
	}
	return cloneProfile(profile), nil
}

func (m *MemoryStore) Update(ctx context.Context, profile Profile, expectedUpdatedAt time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[profile.ID]
	if !ok {
		return errNotFound
	}
	if current.OwnerUserID != profile.OwnerUserID {
		return errConflict
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[profile.ID] = cloneProfile(profile)
	return nil
}

func (m *MemoryStore) CreateOfferedService(ctx context.Context, svc OfferedService) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := svc.Validate(); err != nil {
		return err
	}
	if _, ok := m.services[svc.ID]; ok {
		return errConflict
	}
	m.services[svc.ID] = cloneOfferedService(svc)
	return nil
}

func (m *MemoryStore) GetOfferedService(ctx context.Context, id ID) (OfferedService, error) {
	if m == nil {
		return OfferedService{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return OfferedService{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return OfferedService{}, m.fail
	}
	svc, ok := m.services[id]
	if !ok {
		return OfferedService{}, errNotFound
	}
	return cloneOfferedService(svc), nil
}

func (m *MemoryStore) ListOfferedServices(ctx context.Context, businessID ID) ([]OfferedService, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	out := make([]OfferedService, 0)
	for _, svc := range m.services {
		if svc.BusinessID == businessID {
			out = append(out, cloneOfferedService(svc))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
	})
	return out, nil
}

func (m *MemoryStore) UpdateOfferedService(ctx context.Context, svc OfferedService, expectedUpdatedAt time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if err := svc.Validate(); err != nil {
		return err
	}
	current, ok := m.services[svc.ID]
	if !ok {
		return errNotFound
	}
	if current.BusinessID != svc.BusinessID {
		return errConflict
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.services[svc.ID] = cloneOfferedService(svc)
	return nil
}

func (m *MemoryStore) FindServiceCandidates(ctx context.Context, query contracts.CandidateQuery) ([]contracts.ServiceCandidate, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		return []contracts.ServiceCandidate{}, nil
	}
	if err := ValidateCoordinates(Coordinates{Latitude: query.Latitude, Longitude: query.Longitude}); err != nil {
		return nil, err
	}
	if query.RadiusKm <= 0 {
		return nil, errInvalidRadius
	}
	origin := Coordinates{Latitude: query.Latitude, Longitude: query.Longitude}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	type ranked struct {
		c        contracts.ServiceCandidate
		distance float64
		id       ID
	}
	rankedList := make([]ranked, 0)
	for _, svc := range m.services {
		profile, ok := m.byID[svc.BusinessID]
		if !ok {
			continue
		}
		c, distance, ok := matchServiceCandidate(svc, profile, origin, query.RadiusKm, query.CategoryID)
		if !ok {
			continue
		}
		rankedList = append(rankedList, ranked{c: c, distance: distance, id: svc.ID})
	}
	sort.Slice(rankedList, func(i, j int) bool {
		if rankedList[i].distance != rankedList[j].distance {
			return rankedList[i].distance < rankedList[j].distance
		}
		return bytes.Compare(rankedList[i].id[:], rankedList[j].id[:]) < 0
	})
	if query.Limit < len(rankedList) {
		rankedList = rankedList[:query.Limit]
	}
	out := make([]contracts.ServiceCandidate, 0, len(rankedList))
	seen := make(map[ID]struct{}, len(rankedList))
	for _, row := range rankedList {
		if _, ok := seen[row.id]; ok {
			continue
		}
		seen[row.id] = struct{}{}
		out = append(out, row.c)
	}
	return out, nil
}

func (m *MemoryStore) CheckServiceCandidate(ctx context.Context, check contracts.EligibilityCheck) (contracts.ServiceCandidate, error) {
	if m == nil {
		return contracts.ServiceCandidate{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return contracts.ServiceCandidate{}, err
	}
	if check.BusinessID.IsZero() || check.ServiceID.IsZero() {
		return contracts.ServiceCandidate{}, errZeroID
	}
	if err := ValidateCoordinates(Coordinates{Latitude: check.Latitude, Longitude: check.Longitude}); err != nil {
		return contracts.ServiceCandidate{}, err
	}
	if check.RadiusKm <= 0 {
		return contracts.ServiceCandidate{}, errInvalidRadius
	}
	origin := Coordinates{Latitude: check.Latitude, Longitude: check.Longitude}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return contracts.ServiceCandidate{}, m.fail
	}
	svc, ok := m.services[ID(check.ServiceID)]
	if !ok || svc.BusinessID != ID(check.BusinessID) {
		return contracts.ServiceCandidate{}, errNotFound
	}
	profile, ok := m.byID[svc.BusinessID]
	if !ok {
		return contracts.ServiceCandidate{}, errNotFound
	}
	c, _, ok := matchServiceCandidate(svc, profile, origin, check.RadiusKm, check.CategoryID)
	if !ok {
		return contracts.ServiceCandidate{}, errNotFound
	}
	return c, nil
}

func matchServiceCandidate(svc OfferedService, profile Profile, origin Coordinates, radiusKm float64, categoryID *contracts.ID) (contracts.ServiceCandidate, float64, bool) {
	if svc.Status != ServiceStatusActive {
		return contracts.ServiceCandidate{}, 0, false
	}
	if profile.Status != StatusActive || profile.Location == nil {
		return contracts.ServiceCandidate{}, 0, false
	}
	if categoryID != nil {
		if svc.CategoryID == nil || ID(*categoryID) != *svc.CategoryID {
			return contracts.ServiceCandidate{}, 0, false
		}
	}
	distance := haversineKm(origin, *profile.Location)
	if distance > radiusKm {
		return contracts.ServiceCandidate{}, 0, false
	}
	amount, currency := clonePrice(svc.Price.Amount, svc.Price.Currency)
	c := contracts.ServiceCandidate{
		BusinessID:          contracts.ID(profile.ID),
		BusinessDisplayName: profile.DisplayName,
		ServiceID:           contracts.ID(svc.ID),
		ServiceTitle:        svc.Title,
		ServiceDescription:  svc.Description,
		PriceModel:          string(svc.Price.Model),
		PriceAmount:         amount,
		PriceCurrency:       currency,
		DistanceKm:          roundDistanceKm(distance),
	}
	if svc.CategoryID != nil {
		id := contracts.ID(*svc.CategoryID)
		c.CategoryID = &id
	}
	return c, distance, true
}

var _ store = (*MemoryStore)(nil)
