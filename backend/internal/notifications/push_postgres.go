package notifications

import (
	"context"
	"time"

	"backend/internal/notifications/policy"
)

const pushEndpointSelect = `id, user_id, channel, platform, provider, endpoint_hash,
	endpoint_key_id, endpoint_nonce, endpoint_ciphertext,
	created_at, updated_at, last_seen_at, revoked_at`

func (p *PostgresStore) GetPushEndpointByHash(ctx context.Context, hash []byte) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	if len(hash) != 32 {
		return PushEndpointRecord{}, policy.ErrInvalidInput
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+pushEndpointSelect+`
		FROM notifications.push_endpoints
		WHERE endpoint_hash = $1 AND revoked_at IS NULL`, hash)
	return scanPushEndpoint(row)
}

func (p *PostgresStore) GetPushEndpointByHashAny(ctx context.Context, userID ID, hash []byte) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	if userID.IsZero() || len(hash) != 32 {
		return PushEndpointRecord{}, policy.ErrInvalidInput
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+pushEndpointSelect+`
		FROM notifications.push_endpoints
		WHERE user_id = $1 AND endpoint_hash = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, userID, hash)
	return scanPushEndpoint(row)
}

func (p *PostgresStore) InsertPushEndpoint(ctx context.Context, row PushEndpointRecord) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	got := p.db.QueryRow(ctx, `
		INSERT INTO notifications.push_endpoints (
			id, user_id, channel, platform, provider, endpoint_hash,
			endpoint_key_id, endpoint_nonce, endpoint_ciphertext,
			created_at, updated_at, last_seen_at, revoked_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING `+pushEndpointSelect,
		row.ID, row.UserID, string(row.Channel), string(row.Platform), string(row.Provider),
		row.Hash, row.KeyID, row.Nonce, row.Ciphertext,
		row.CreatedAt, row.UpdatedAt, row.LastSeenAt, row.RevokedAt,
	)
	return scanPushEndpoint(got)
}

func (p *PostgresStore) RefreshPushEndpoint(ctx context.Context, id, userID ID, keyID string, nonce, ciphertext []byte, now time.Time) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE notifications.push_endpoints SET
			endpoint_key_id = $3,
			endpoint_nonce = $4,
			endpoint_ciphertext = $5,
			updated_at = $6,
			last_seen_at = $6,
			revoked_at = NULL
		WHERE id = $1 AND user_id = $2
		RETURNING `+pushEndpointSelect, id, userID, keyID, nonce, ciphertext, now)
	return scanPushEndpoint(row)
}

func (p *PostgresStore) RevokePushEndpoint(ctx context.Context, id, userID ID, now time.Time) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE notifications.push_endpoints SET
			revoked_at = COALESCE(revoked_at, $3),
			updated_at = CASE WHEN revoked_at IS NULL THEN $3 ELSE updated_at END
		WHERE id = $1 AND user_id = $2
		RETURNING `+pushEndpointSelect, id, userID, now)
	return scanPushEndpoint(row)
}

func (p *PostgresStore) GetPushEndpoint(ctx context.Context, id, userID ID) (PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return PushEndpointRecord{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+pushEndpointSelect+`
		FROM notifications.push_endpoints
		WHERE id = $1 AND user_id = $2`, id, userID)
	return scanPushEndpoint(row)
}

func (p *PostgresStore) ListPushEndpoints(ctx context.Context, userID ID) ([]PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+pushEndpointSelect+`
		FROM notifications.push_endpoints
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY last_seen_at DESC, id DESC`, userID)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]PushEndpointRecord, 0)
	for rows.Next() {
		rec, err := scanPushEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) HasActivePushEndpoint(ctx context.Context, userID ID, ch policy.Channel) (bool, error) {
	if p == nil || p.db == nil {
		return false, errUnavailable
	}
	if userID.IsZero() {
		return false, policy.ErrInvalidActor
	}
	row := p.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM notifications.push_endpoints
			WHERE user_id = $1 AND channel = $2 AND revoked_at IS NULL
		)`, userID, string(ch))
	var ok bool
	if err := row.Scan(&ok); err != nil {
		return false, mapPolicyDBErr(err)
	}
	return ok, nil
}

func (p *PostgresStore) ListActivePushForDispatch(ctx context.Context, userID ID, ch policy.Channel) ([]PushEndpointRecord, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+pushEndpointSelect+`
		FROM notifications.push_endpoints
		WHERE user_id = $1 AND channel = $2 AND revoked_at IS NULL
		ORDER BY last_seen_at DESC, id DESC`, userID, string(ch))
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]PushEndpointRecord, 0)
	for rows.Next() {
		rec, err := scanPushEndpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func scanPushEndpoint(row scanner) (PushEndpointRecord, error) {
	var rec PushEndpointRecord
	var ch, platform, provider string
	if err := row.Scan(
		&rec.ID, &rec.UserID, &ch, &platform, &provider, &rec.Hash,
		&rec.KeyID, &rec.Nonce, &rec.Ciphertext,
		&rec.CreatedAt, &rec.UpdatedAt, &rec.LastSeenAt, &rec.RevokedAt,
	); err != nil {
		return PushEndpointRecord{}, mapPolicyDBErr(err)
	}
	rec.Channel = policy.Channel(ch)
	rec.Platform = PushPlatform(platform)
	rec.Provider = PushProvider(provider)
	return rec, nil
}
