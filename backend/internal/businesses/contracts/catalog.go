package contracts

import "context"

// OfferedServiceRef is the minimal cross-domain catalog surface.
// It does not include owner UUID or internal timestamps.
type OfferedServiceRef struct {
	ID         ID
	BusinessID ID
	Status     string
	Title      string
}

// Catalog is the Businesses-owned services read surface. Callers must not import businesses impl.
type Catalog interface {
	GetService(ctx context.Context, serviceID ID) (OfferedServiceRef, error)
	ListServicesForBusiness(ctx context.Context, businessID ID) ([]OfferedServiceRef, error)
}
