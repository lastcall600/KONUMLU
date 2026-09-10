package search

import "context"

type documentStore interface {
	Upsert(ctx context.Context, doc ListingDocument) error
	Get(ctx context.Context, listingID ID) (ListingDocument, error)
	Remove(ctx context.Context, listingID ID) error
}

type listingQueryStore interface {
	Search(ctx context.Context, q NormalizedQuery) ([]Hit, error)
}

type Hit struct {
	Doc  ListingDocument
	Rank float64
}
