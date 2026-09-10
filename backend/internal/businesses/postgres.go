package businesses

import (
	"context"
	"errors"
	"strconv"
	"time"

	"backend/internal/platform/db"
)

var _ store = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const profileSelectCols = `id, owner_user_id, display_name, description, status,
	ST_Y(location::geometry) AS latitude, ST_X(location::geometry) AS longitude,
	created_at, updated_at`

func (p *PostgresStore) Create(ctx context.Context, profile Profile) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO businesses.profiles (
			id, owner_user_id, display_name, description, status, location, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,`+locationWriteSQL(6)+`,$8,$9)`,
		profile.ID, profile.OwnerUserID, profile.DisplayName, nullIfEmpty(profile.Description),
		string(profile.Status), locationLon(profile.Location), locationLat(profile.Location),
		profile.CreatedAt, profile.UpdatedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Profile, error) {
	if p == nil || p.db == nil {
		return Profile{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+profileSelectCols+`
		FROM businesses.profiles
		WHERE id = $1`, id)
	got, err := scanProfile(row)
	if err != nil {
		return Profile{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) GetByOwner(ctx context.Context, ownerUserID ID) (Profile, error) {
	if p == nil || p.db == nil {
		return Profile{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+profileSelectCols+`
		FROM businesses.profiles
		WHERE owner_user_id = $1`, ownerUserID)
	got, err := scanProfile(row)
	if err != nil {
		return Profile{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Update(ctx context.Context, profile Profile, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	n, err := p.db.Exec(ctx, `
		UPDATE businesses.profiles SET
			display_name = $2,
			description = $3,
			status = $4,
			location = `+locationWriteSQL(5)+`,
			updated_at = $7
		WHERE id = $1 AND owner_user_id = $8 AND updated_at = $9`,
		profile.ID, profile.DisplayName, nullIfEmpty(profile.Description), string(profile.Status),
		locationLon(profile.Location), locationLat(profile.Location),
		profile.UpdatedAt, profile.OwnerUserID, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, profile.ID)
		if errors.Is(getErr, errNotFound) {
			return errNotFound
		}
		if getErr != nil {
			return getErr
		}
		return errConflict
	}
	return nil
}

func scanProfile(row interface {
	Scan(dest ...any) error
}) (Profile, error) {
	var p Profile
	var status string
	var description *string
	var lat, lon *float64
	if err := row.Scan(
		&p.ID, &p.OwnerUserID, &p.DisplayName, &description, &status, &lat, &lon, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return Profile{}, err
	}
	if description != nil {
		p.Description = *description
	}
	p.Status = Status(status)
	if lat != nil && lon != nil {
		p.Location = &Coordinates{Latitude: *lat, Longitude: *lon}
	}
	return p, nil
}

func locationWriteSQL(lonArg int) string {
	lon := strconv.Itoa(lonArg)
	lat := strconv.Itoa(lonArg + 1)
	return `CASE WHEN $` + lon + `::float8 IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($` + lon + `, $` + lat + `), 4326)::geography END`
}

func locationLon(c *Coordinates) any {
	if c == nil {
		return nil
	}
	return c.Longitude
}

func locationLat(c *Coordinates) any {
	if c == nil {
		return nil
	}
	return c.Latitude
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
