package contracts

import "context"

// CandidateQuery is the Needs-owned matching request. Ranking and radius are
// server-defined; callers must not treat client query strings as ranking input.
type CandidateQuery struct {
	Latitude   float64
	Longitude  float64
	RadiusKm   float64
	CategoryID *ID
	Limit      int
}

// ServiceCandidate is a public-safe, query-time match row.
// It never includes owner or requester user UUIDs.
type ServiceCandidate struct {
	BusinessID          ID
	BusinessDisplayName string
	ServiceID           ID
	ServiceTitle        string
	ServiceDescription  string
	PriceModel          string
	PriceAmount         *string
	PriceCurrency       *string
	DistanceKm          float64
	CategoryID          *ID
}

// CandidateDiscovery is the Businesses-owned candidate read surface.
// Needs must not query businesses tables directly.
type CandidateDiscovery interface {
	FindServiceCandidates(ctx context.Context, query CandidateQuery) ([]ServiceCandidate, error)
}

// EligibilityCheck asks whether one business+service is a valid Need candidate
// under the same category/location rules as FindServiceCandidates, without a list limit.
type EligibilityCheck struct {
	BusinessID ID
	ServiceID  ID
	Latitude   float64
	Longitude  float64
	RadiusKm   float64
	CategoryID *ID
}

// OfferEligibility is the narrow Businesses-owned check used when submitting an offer.
// Callers must not reimplement matching predicates.
type OfferEligibility interface {
	CheckServiceCandidate(ctx context.Context, check EligibilityCheck) (ServiceCandidate, error)
}
