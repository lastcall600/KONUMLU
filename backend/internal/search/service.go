package search

import "context"

// Service runs public listing discovery against the derived projection.
type Service struct {
	store listingQueryStore
}

func NewService(store listingQueryStore) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	return &Service{store: store}, nil
}

func (s *Service) SearchListings(ctx context.Context, q Query) (Page, error) {
	if s == nil || s.store == nil {
		return Page{}, errStoreRequired
	}
	nq, err := q.normalize()
	if err != nil {
		return Page{}, err
	}
	hits, err := s.store.Search(ctx, nq)
	if err != nil {
		return Page{}, mapStoreErr(err)
	}
	page := Page{Hits: hits}
	if len(hits) > nq.Limit {
		cursor, err := encodeCursor(hits[nq.Limit-1], nq.HasText)
		if err != nil {
			return Page{}, err
		}
		page.Hits = hits[:nq.Limit]
		page.NextCursor = cursor
	}
	return page, nil
}
