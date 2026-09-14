package notifications

import (
	"context"
	"time"

	"backend/internal/notifications/policy"
)

const channelDeliverySelect = `id, intent_id, channel, state, attempts, next_attempt_at, suppression_reason,
	last_error_class, provider_ref, created_at, updated_at, completed_at`

const claimChannelSQL = `
WITH picked AS (
	SELECT id
	FROM notifications.channel_deliveries
	WHERE channel = ANY($4::text[])
	  AND (
		(state IN ('pending', 'retryable_failed') AND (next_attempt_at IS NULL OR next_attempt_at <= $1))
		OR (state = 'processing' AND updated_at <= $3)
	  )
	ORDER BY COALESCE(next_attempt_at, updated_at), id
	FOR UPDATE SKIP LOCKED
	LIMIT $2
)
UPDATE notifications.channel_deliveries d
SET state = 'processing',
    attempts = attempts + 1,
    updated_at = $1
FROM picked
WHERE d.id = picked.id
RETURNING d.id, d.intent_id, d.channel, d.state, d.attempts, d.next_attempt_at, d.suppression_reason,
	d.last_error_class, d.provider_ref, d.created_at, d.updated_at, d.completed_at`

func (p *PostgresStore) GetIntent(ctx context.Context, id ID) (SemanticIntent, error) {
	if p == nil || p.db == nil {
		return SemanticIntent{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `SELECT `+intentSelectCols+` FROM notifications.intents WHERE id = $1`, id)
	return scanIntent(row)
}

func (p *PostgresStore) ClaimChannelDeliveries(ctx context.Context, now time.Time, hold time.Duration, limit int, channels []policy.Channel) ([]ChannelDeliveryRow, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if hold <= 0 || limit <= 0 || len(channels) == 0 {
		return nil, errInvalidQuery
	}
	ch := make([]string, 0, len(channels))
	for _, c := range channels {
		ch = append(ch, string(c))
	}
	stale := now.Add(-hold)
	rows, err := p.db.Query(ctx, claimChannelSQL, now.UTC(), limit, stale.UTC(), ch)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]ChannelDeliveryRow, 0)
	for rows.Next() {
		row, err := scanChannelDeliveryFull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) FinishChannelDelivery(ctx context.Context, row ChannelDeliveryRow) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	var reason *string
	if row.SuppressionReason != nil {
		s := string(*row.SuppressionReason)
		reason = &s
	}
	n, err := p.db.Exec(ctx, `
		UPDATE notifications.channel_deliveries
		SET state = $2,
		    next_attempt_at = $3,
		    suppression_reason = $4,
		    last_error_class = $5,
		    provider_ref = $6,
		    updated_at = $7,
		    completed_at = $8
		WHERE id = $1 AND state = 'processing'`,
		row.ID, string(row.State), row.NextAttemptAt, reason, row.LastErrorClass, row.ProviderRef,
		row.UpdatedAt.UTC(), row.CompletedAt)
	if err != nil {
		return mapPolicyDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func (p *PostgresStore) CountChannelState(ctx context.Context, state policy.DeliveryState) (int, error) {
	if p == nil || p.db == nil {
		return 0, errUnavailable
	}
	row := p.db.QueryRow(ctx, `SELECT COUNT(*) FROM notifications.channel_deliveries WHERE state = $1`, string(state))
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, mapPolicyDBErr(err)
	}
	return n, nil
}

func scanChannelDeliveryFull(row scanner) (ChannelDeliveryRow, error) {
	var d ChannelDeliveryRow
	var ch, state string
	var reason *string
	if err := row.Scan(
		&d.ID, &d.IntentID, &ch, &state, &d.Attempts, &d.NextAttemptAt, &reason,
		&d.LastErrorClass, &d.ProviderRef, &d.CreatedAt, &d.UpdatedAt, &d.CompletedAt,
	); err != nil {
		return ChannelDeliveryRow{}, mapPolicyDBErr(err)
	}
	d.Channel = policy.Channel(ch)
	d.State = policy.DeliveryState(state)
	if reason != nil {
		r := policy.SuppressionReason(*reason)
		d.SuppressionReason = &r
	}
	return d, nil
}
