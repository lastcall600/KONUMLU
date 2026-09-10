package contracts

import "context"

// PublishedCategoryLookup is the Master Data read Needs may call for optional categoryId.
// It does not expose taxonomy tables or unpublished nodes.
type PublishedCategoryLookup interface {
	RequirePublished(ctx context.Context, categoryID ID) error
}
