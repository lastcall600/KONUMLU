package businesses

import (
	"context"

	"backend/internal/businesses/contracts"
)

func (p *PostgresStore) FindServiceCandidates(ctx context.Context, query contracts.CandidateQuery) ([]contracts.ServiceCandidate, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if query.Limit <= 0 {
		return []contracts.ServiceCandidate{}, nil
	}
	if err := ValidateCoordinates(Coordinates{Latitude: query.Latitude, Longitude: query.Longitude}); err != nil {
		return nil, err
	}
	if query.RadiusKm <= 0 || query.RadiusKm != query.RadiusKm {
		return nil, errInvalidRadius
	}
	meters := query.RadiusKm * 1000
	rows, err := p.db.Query(ctx, `
		SELECT
			s.id,
			s.business_id,
			p.display_name,
			s.title,
			s.description,
			s.price_model,
			s.price_amount::text,
			s.price_currency,
			s.category_id,
			ST_Distance(
				p.location,
				ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
			) / 1000.0 AS distance_km
		FROM businesses.services s
		INNER JOIN businesses.profiles p ON p.id = s.business_id
		WHERE p.status = 'active'
		  AND s.status = 'active'
		  AND p.location IS NOT NULL
		  AND ST_DWithin(
				p.location,
				ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
				$3
		  )
		  AND ($4::uuid IS NULL OR s.category_id = $4)
		ORDER BY distance_km ASC, s.id ASC
		LIMIT $5`,
		query.Longitude, query.Latitude, meters, candidateCategoryArg(query.CategoryID), query.Limit,
	)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]contracts.ServiceCandidate, 0)
	seen := make(map[ID]struct{})
	for rows.Next() {
		var c contracts.ServiceCandidate
		var desc *string
		var model *string
		var category *contracts.ID
		if err := rows.Scan(
			&c.ServiceID, &c.BusinessID, &c.BusinessDisplayName, &c.ServiceTitle, &desc,
			&model, &c.PriceAmount, &c.PriceCurrency, &category, &c.DistanceKm,
		); err != nil {
			return nil, mapDBErr(err)
		}
		if _, dup := seen[ID(c.ServiceID)]; dup {
			continue
		}
		seen[ID(c.ServiceID)] = struct{}{}
		if desc != nil {
			c.ServiceDescription = *desc
		}
		if model != nil {
			c.PriceModel = *model
		}
		c.CategoryID = category
		c.DistanceKm = roundDistanceKm(c.DistanceKm)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) CheckServiceCandidate(ctx context.Context, check contracts.EligibilityCheck) (contracts.ServiceCandidate, error) {
	if p == nil || p.db == nil {
		return contracts.ServiceCandidate{}, errUnavailable
	}
	if check.BusinessID.IsZero() || check.ServiceID.IsZero() {
		return contracts.ServiceCandidate{}, errZeroID
	}
	if err := ValidateCoordinates(Coordinates{Latitude: check.Latitude, Longitude: check.Longitude}); err != nil {
		return contracts.ServiceCandidate{}, err
	}
	if check.RadiusKm <= 0 || check.RadiusKm != check.RadiusKm {
		return contracts.ServiceCandidate{}, errInvalidRadius
	}
	meters := check.RadiusKm * 1000
	row := p.db.QueryRow(ctx, `
		SELECT
			s.id,
			s.business_id,
			p.display_name,
			s.title,
			s.description,
			s.price_model,
			s.price_amount::text,
			s.price_currency,
			s.category_id,
			ST_Distance(
				p.location,
				ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
			) / 1000.0 AS distance_km
		FROM businesses.services s
		INNER JOIN businesses.profiles p ON p.id = s.business_id
		WHERE s.id = $3
		  AND s.business_id = $4
		  AND p.status = 'active'
		  AND s.status = 'active'
		  AND p.location IS NOT NULL
		  AND ST_DWithin(
				p.location,
				ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
				$5
		  )
		  AND ($6::uuid IS NULL OR s.category_id = $6)`,
		check.Longitude, check.Latitude, check.ServiceID, check.BusinessID, meters, candidateCategoryArg(check.CategoryID),
	)
	var c contracts.ServiceCandidate
	var desc *string
	var model *string
	var category *contracts.ID
	if err := row.Scan(
		&c.ServiceID, &c.BusinessID, &c.BusinessDisplayName, &c.ServiceTitle, &desc,
		&model, &c.PriceAmount, &c.PriceCurrency, &category, &c.DistanceKm,
	); err != nil {
		return contracts.ServiceCandidate{}, mapDBErr(err)
	}
	if desc != nil {
		c.ServiceDescription = *desc
	}
	if model != nil {
		c.PriceModel = *model
	}
	c.CategoryID = category
	c.DistanceKm = roundDistanceKm(c.DistanceKm)
	return c, nil
}

func candidateCategoryArg(id *contracts.ID) any {
	if id == nil {
		return nil
	}
	return *id
}
