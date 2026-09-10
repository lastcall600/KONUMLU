package favorites

import "context"

type favoriteStore interface {
	Add(ctx context.Context, fav Favorite) error
	Remove(ctx context.Context, userID, listingID ID) error
	Get(ctx context.Context, userID, listingID ID) (Favorite, error)
	ListByUser(ctx context.Context, userID ID) ([]Favorite, error)
}
