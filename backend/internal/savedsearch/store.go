package savedsearch

import "context"

type savedSearchStore interface {
	Insert(ctx context.Context, row SavedSearch) error
	GetByUser(ctx context.Context, userID, id ID) (SavedSearch, error)
	ListByUser(ctx context.Context, userID ID) ([]SavedSearch, error)
	DeleteByUser(ctx context.Context, userID, id ID) error
}
