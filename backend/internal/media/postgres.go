package media

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

var _ assetStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const assetSelectCols = `id, owner_user_id, listing_id, kind, status, object_key, processed_object_key,
	original_filename, content_type, size_bytes, width, height, sort_order,
	created_at, updated_at, ready_at, rejected_at, deleted_at`

func (p *PostgresStore) Create(ctx context.Context, asset Asset) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := asset.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO media.assets (
			id, owner_user_id, listing_id, kind, status, object_key, processed_object_key,
			original_filename, content_type, size_bytes, width, height, sort_order,
			created_at, updated_at, ready_at, rejected_at, deleted_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		asset.ID, asset.OwnerUserID, idArg(asset.ListingID), string(asset.Kind), string(asset.Status), asset.ObjectKey, processedKeyArg(asset.ProcessedObjectKey),
		asset.OriginalFilename, asset.ContentType, asset.SizeBytes, asset.Width, asset.Height, asset.SortOrder,
		asset.CreatedAt, asset.UpdatedAt, asset.ReadyAt, asset.RejectedAt, asset.DeletedAt,
	)
	return mapDBErr(err)
}

func (p *PostgresStore) Get(ctx context.Context, id ID) (Asset, error) {
	if p == nil || p.db == nil {
		return Asset{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+assetSelectCols+`
		FROM media.assets
		WHERE id = $1`, id)
	got, err := scanAsset(row)
	if err != nil {
		return Asset{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) Begin(ctx context.Context) (transaction, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return nil, mapDBErr(err)
	}
	return tx, nil
}

func (p *PostgresStore) Update(ctx context.Context, asset Asset, expectedUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	return p.update(ctx, p.db, asset, expectedUpdatedAt)
}

func (p *PostgresStore) UpdateInTx(ctx context.Context, exec outbox.Execer, asset Asset, expectedUpdatedAt time.Time) error {
	if exec == nil {
		return p.Update(ctx, asset, expectedUpdatedAt)
	}
	writer, ok := exec.(assetWriter)
	if !ok {
		return errUnavailable
	}
	return p.update(ctx, writer, asset, expectedUpdatedAt)
}

type assetWriter interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	QueryRow(ctx context.Context, sql string, args ...any) db.Row
}

func (p *PostgresStore) update(ctx context.Context, w assetWriter, asset Asset, expectedUpdatedAt time.Time) error {
	if w == nil {
		return errUnavailable
	}
	if err := asset.Validate(); err != nil {
		return err
	}
	n, err := w.Exec(ctx, `
		UPDATE media.assets SET
			listing_id = $2,
			kind = $3,
			status = $4,
			object_key = $5,
			processed_object_key = $6,
			original_filename = $7,
			content_type = $8,
			size_bytes = $9,
			width = $10,
			height = $11,
			sort_order = $12,
			updated_at = $13,
			ready_at = $14,
			rejected_at = $15,
			deleted_at = $16
		WHERE id = $1 AND updated_at = $17`,
		asset.ID, idArg(asset.ListingID), string(asset.Kind), string(asset.Status), asset.ObjectKey, processedKeyArg(asset.ProcessedObjectKey),
		asset.OriginalFilename, asset.ContentType, asset.SizeBytes, asset.Width, asset.Height, asset.SortOrder,
		asset.UpdatedAt, asset.ReadyAt, asset.RejectedAt, asset.DeletedAt, expectedUpdatedAt,
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		_, getErr := p.Get(ctx, asset.ID)
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

func (p *PostgresStore) ListByListing(ctx context.Context, listingID ID) ([]Asset, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+assetSelectCols+`
		FROM media.assets
		WHERE listing_id = $1
		ORDER BY sort_order ASC NULLS LAST, created_at ASC, id ASC`, listingID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Asset, 0)
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) ListReclaimable(ctx context.Context, now time.Time, pendingAge, rejectedAge time.Duration, limit int) ([]Asset, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = 50
	}
	pendingCutoff := now.Add(-pendingAge)
	rejectedCutoff := now.Add(-rejectedAge)
	rows, err := p.db.Query(ctx, `
		SELECT `+assetSelectCols+`
		FROM media.assets
		WHERE status <> 'ready'
		  AND (
			(status = 'pending_upload' AND created_at < $1)
			OR (status = 'rejected' AND rejected_at IS NOT NULL AND rejected_at < $2)
		  )
		ORDER BY updated_at ASC, id ASC
		LIMIT $3`, pendingCutoff, rejectedCutoff, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Asset, 0)
	for rows.Next() {
		asset, err := scanAsset(rows)
		if err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, asset)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func processedKeyArg(key string) any {
	if key == "" {
		return nil
	}
	return key
}

func idArg(id *ID) any {
	if id == nil {
		return nil
	}
	return *id
}

func scanAsset(row interface {
	Scan(dest ...any) error
}) (Asset, error) {
	var a Asset
	var kind, status string
	var listingID *ID
	var processed *string
	if err := row.Scan(
		&a.ID, &a.OwnerUserID, &listingID, &kind, &status, &a.ObjectKey, &processed,
		&a.OriginalFilename, &a.ContentType, &a.SizeBytes, &a.Width, &a.Height, &a.SortOrder,
		&a.CreatedAt, &a.UpdatedAt, &a.ReadyAt, &a.RejectedAt, &a.DeletedAt,
	); err != nil {
		return Asset{}, err
	}
	if processed != nil {
		a.ProcessedObjectKey = *processed
	}
	a.ListingID = cloneID(listingID)
	a.Kind = Kind(kind)
	a.Status = Status(status)
	return a, nil
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
