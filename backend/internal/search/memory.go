package search

import (
	"context"
	"sort"
	"sync"
)

// MemoryStore is an in-process derived projection for tests.
type MemoryStore struct {
	mu   sync.Mutex
	byID map[ID]ListingDocument
	fail error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[ID]ListingDocument)}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) Upsert(ctx context.Context, doc ListingDocument) error {
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
	if err := doc.Validate(); err != nil {
		return err
	}
	m.byID[doc.ListingID] = cloneDoc(doc)
	return nil
}

func (m *MemoryStore) Get(ctx context.Context, listingID ID) (ListingDocument, error) {
	if m == nil {
		return ListingDocument{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return ListingDocument{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return ListingDocument{}, m.fail
	}
	doc, ok := m.byID[listingID]
	if !ok {
		return ListingDocument{}, errNotFound
	}
	return cloneDoc(doc), nil
}

func (m *MemoryStore) Remove(ctx context.Context, listingID ID) error {
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
	delete(m.byID, listingID)
	return nil
}

func (m *MemoryStore) Search(ctx context.Context, q NormalizedQuery) ([]Hit, error) {
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
	hits := make([]Hit, 0, len(m.byID))
	for _, doc := range m.byID {
		if !doc.Searchable() {
			continue
		}
		hit, ok := memoryMatch(cloneDoc(doc), q)
		if !ok {
			continue
		}
		if !afterCursor(hit, q) {
			continue
		}
		hits = append(hits, hit)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return cmpHits(hits[i], hits[j], q.HasText)
	})
	limit := q.Limit + 1
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, nil
}

func memoryMatch(doc ListingDocument, q NormalizedQuery) (Hit, bool) {
	if q.CategoryID != nil && doc.CategoryID != *q.CategoryID {
		return Hit{}, false
	}
	if q.Currency != nil {
		if doc.PriceCurrency == nil || *doc.PriceCurrency != *q.Currency {
			return Hit{}, false
		}
	}
	if q.MinPrice != nil || q.MaxPrice != nil {
		amount := priceRat(doc.PriceAmount)
		if amount == nil {
			return Hit{}, false
		}
		if q.MinPrice != nil && amount.Cmp(q.MinPrice) < 0 {
			return Hit{}, false
		}
		if q.MaxPrice != nil && amount.Cmp(q.MaxPrice) > 0 {
			return Hit{}, false
		}
	}
	if q.Viewport != nil {
		if doc.Latitude == nil || doc.Longitude == nil {
			return Hit{}, false
		}
		lat, lon := *doc.Latitude, *doc.Longitude
		if lat < q.Viewport.South || lat > q.Viewport.North || lon < q.Viewport.West || lon > q.Viewport.East {
			return Hit{}, false
		}
	}
	hit := Hit{Doc: doc}
	if q.HasText {
		rank, ok := memoryTextRank(doc.Title, doc.Description, q.Tokens)
		if !ok {
			return Hit{}, false
		}
		hit.Rank = rank
	} else if doc.PublishedAt == nil {
		return Hit{}, false
	}
	return hit, true
}

var _ documentStore = (*MemoryStore)(nil)
var _ listingQueryStore = (*MemoryStore)(nil)
