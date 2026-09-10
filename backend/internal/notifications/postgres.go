package notifications

import (
	"context"
	"errors"

	"backend/internal/notifications/contracts"
	"backend/internal/platform/db"
)

var _ deliveryStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const deliverySelectCols = `id, intent_id, channel, template_code, template_version, locale,
	recipient_kind, recipient_id, status, attempts, provider_ref, correlation_id,
	created_at, updated_at, last_attempt_at, completed_at`

func (p *PostgresStore) UpsertDelivery(ctx context.Context, delivery Delivery) (Delivery, error) {
	if p == nil || p.db == nil {
		return Delivery{}, errUnavailable
	}
	if err := delivery.Validate(); err != nil {
		return Delivery{}, err
	}
	row := p.db.QueryRow(ctx, `
		INSERT INTO notifications.deliveries (
			id, intent_id, channel, template_code, template_version, locale,
			recipient_kind, recipient_id, status, attempts, provider_ref, correlation_id,
			created_at, updated_at, last_attempt_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (intent_id) DO UPDATE SET
			updated_at = EXCLUDED.updated_at
		RETURNING `+deliverySelectCols,
		delivery.ID, delivery.IntentID, string(delivery.Channel), delivery.TemplateCode, delivery.TemplateVersion, delivery.Locale,
		string(delivery.RecipientKind), delivery.RecipientID, string(delivery.Status), delivery.Attempts, delivery.ProviderRef, delivery.CorrelationID,
		delivery.CreatedAt, delivery.UpdatedAt, delivery.LastAttemptAt, delivery.CompletedAt,
	)
	got, err := scanDelivery(row)
	if err != nil {
		return Delivery{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) SaveDelivery(ctx context.Context, delivery Delivery) (Delivery, error) {
	if p == nil || p.db == nil {
		return Delivery{}, errUnavailable
	}
	if err := delivery.Validate(); err != nil {
		return Delivery{}, err
	}
	row := p.db.QueryRow(ctx, `
		UPDATE notifications.deliveries SET
			status = $2,
			attempts = $3,
			provider_ref = $4,
			updated_at = $5,
			last_attempt_at = $6,
			completed_at = $7
		WHERE id = $1
		RETURNING `+deliverySelectCols,
		delivery.ID, string(delivery.Status), delivery.Attempts, delivery.ProviderRef,
		delivery.UpdatedAt, delivery.LastAttemptAt, delivery.CompletedAt,
	)
	got, err := scanDelivery(row)
	if err != nil {
		return Delivery{}, mapDBErr(err)
	}
	return got, nil
}

func (p *PostgresStore) UpsertWarning(ctx context.Context, row WarningRecord) (WarningRecord, error) {
	if p == nil || p.db == nil {
		return WarningRecord{}, errUnavailable
	}
	if err := row.Validate(); err != nil {
		return WarningRecord{}, err
	}
	scanned := p.db.QueryRow(ctx, `
		INSERT INTO notifications.warning_intents (
			intent_id, locale, template_code, message_key, target_type, target_ref,
			reason_code, action_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (intent_id) DO UPDATE SET
			updated_at = EXCLUDED.updated_at
		RETURNING intent_id, locale, template_code, message_key, target_type, target_ref,
			reason_code, action_at, created_at, updated_at`,
		row.IntentID, row.Locale, row.TemplateCode, row.MessageKey, row.TargetType, row.TargetRef,
		row.ReasonCode, row.ActionAt.UTC(), row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
	)
	got, err := scanWarning(scanned)
	if err != nil {
		return WarningRecord{}, mapDBErr(err)
	}
	return got, nil
}

func scanDelivery(row db.Row) (Delivery, error) {
	var d Delivery
	var channel, kind, status string
	if err := row.Scan(
		&d.ID, &d.IntentID, &channel, &d.TemplateCode, &d.TemplateVersion, &d.Locale,
		&kind, &d.RecipientID, &status, &d.Attempts, &d.ProviderRef, &d.CorrelationID,
		&d.CreatedAt, &d.UpdatedAt, &d.LastAttemptAt, &d.CompletedAt,
	); err != nil {
		return Delivery{}, err
	}
	d.Channel = contracts.Channel(channel)
	d.RecipientKind = contracts.RecipientKind(kind)
	d.Status = Status(status)
	return d, nil
}

func scanWarning(row db.Row) (WarningRecord, error) {
	var w WarningRecord
	if err := row.Scan(
		&w.IntentID, &w.Locale, &w.TemplateCode, &w.MessageKey, &w.TargetType, &w.TargetRef,
		&w.ReasonCode, &w.ActionAt, &w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return WarningRecord{}, err
	}
	return w, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errInvalidDelivery
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
