package offers

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

type MemoryStore struct {
	mu    sync.Mutex
	byID  map[ID]Offer
	fail  error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]Offer)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Create(ctx context.Context, offer Offer) error {
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
	if err := offer.Validate(); err != nil {
		return err
	}
	if _, ok := m.byID[offer.ID]; ok {
		return errConflict
	}
	if offer.Status == StatusSubmitted {
		for _, existing := range m.byID {
			if existing.NeedID == offer.NeedID && existing.ServiceID == offer.ServiceID && existing.Status == StatusSubmitted {
				return errConflict
			}
		}
	}
	if offer.Status == StatusAccepted {
		for _, existing := range m.byID {
			if existing.NeedID == offer.NeedID && existing.Status == StatusAccepted {
				return errConflict
			}
		}
	}
	m.byID[offer.ID] = cloneOffer(offer)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, id ID) (Offer, error) {
	if m == nil {
		return Offer{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Offer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Offer{}, m.fail
	}
	offer, ok := m.byID[id]
	if !ok {
		return Offer{}, errNotFound
	}
	return cloneOffer(offer), nil
}

func (m *MemoryStore) ListByNeed(ctx context.Context, needID ID) ([]Offer, error) {
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
	out := make([]Offer, 0)
	for _, offer := range m.byID {
		if offer.NeedID == needID {
			out = append(out, cloneOffer(offer))
		}
	}
	sortOffers(out)
	return out, nil
}

func (m *MemoryStore) ListByProvider(ctx context.Context, providerUserID ID) ([]Offer, error) {
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
	out := make([]Offer, 0)
	for _, offer := range m.byID {
		if offer.ProviderUserID == providerUserID {
			out = append(out, cloneOffer(offer))
		}
	}
	sortOffers(out)
	return out, nil
}

func (m *MemoryStore) FindSubmitted(ctx context.Context, needID, serviceID ID) (Offer, error) {
	if m == nil {
		return Offer{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Offer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Offer{}, m.fail
	}
	for _, offer := range m.byID {
		if offer.NeedID == needID && offer.ServiceID == serviceID && offer.Status == StatusSubmitted {
			return cloneOffer(offer), nil
		}
	}
	return Offer{}, errNotFound
}

func (m *MemoryStore) Update(ctx context.Context, offer Offer, expectedUpdatedAt time.Time) error {
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
	if err := offer.Validate(); err != nil {
		return err
	}
	current, ok := m.byID[offer.ID]
	if !ok {
		return errNotFound
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return errConflict
	}
	m.byID[offer.ID] = cloneOffer(offer)
	return nil
}

func (m *MemoryStore) AcceptExclusive(ctx context.Context, offerID, needID ID, now time.Time) (Offer, error) {
	if m == nil {
		return Offer{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Offer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Offer{}, m.fail
	}
	current, ok := m.byID[offerID]
	if !ok || current.NeedID != needID {
		return Offer{}, errNotFound
	}
	if current.Status == StatusAccepted {
		return cloneOffer(current), nil
	}
	for _, other := range m.byID {
		if other.NeedID == needID && other.Status == StatusAccepted {
			return Offer{}, errConflict
		}
	}
	next, err := current.Accept(now)
	if err != nil {
		return Offer{}, err
	}
	m.byID[offerID] = cloneOffer(next)
	for id, other := range m.byID {
		if id == offerID || other.NeedID != needID || other.Status != StatusSubmitted {
			continue
		}
		rejected, err := other.Reject(now)
		if err != nil {
			return Offer{}, err
		}
		m.byID[id] = cloneOffer(rejected)
	}
	return cloneOffer(next), nil
}

func sortOffers(out []Offer) {
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
}

var _ store = (*MemoryStore)(nil)
