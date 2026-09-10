package verified

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"backend/internal/platform/db"
	"backend/internal/platform/outbox"
)

var _ appointmentStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) InsertAppointment(ctx context.Context, appt Appointment) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := appt.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO verified.appointments (
			id, listing_id, requester_user_id, provider_user_id, status, requested_at, scheduled_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		appt.ID, appt.ListingID, appt.RequesterUserID, appt.ProviderUserID, appt.Status,
		appt.RequestedAt.UTC(), appt.ScheduledAt, appt.UpdatedAt.UTC(),
	)
	if errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	return mapDBErr(err)
}

func (p *PostgresStore) GetAppointment(ctx context.Context, id ID) (Appointment, error) {
	if p == nil || p.db == nil {
		return Appointment{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT a.id, a.listing_id, a.requester_user_id, a.provider_user_id, a.status,
			a.requested_at, a.scheduled_at, a.updated_at, i.id
		FROM verified.appointments a
		LEFT JOIN verified.verified_interactions i ON i.appointment_id = a.id
		WHERE a.id = $1`, id)
	return scanAppointmentRead(row)
}

func (p *PostgresStore) ListAppointmentsForUser(ctx context.Context, userID ID, limit int) ([]Appointment, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = MaxAppointments
	}
	rows, err := p.db.Query(ctx, `
		SELECT a.id, a.listing_id, a.requester_user_id, a.provider_user_id, a.status,
			a.requested_at, a.scheduled_at, a.updated_at, i.id
		FROM verified.appointments a
		LEFT JOIN verified.verified_interactions i ON i.appointment_id = a.id
		WHERE a.requester_user_id = $1 OR a.provider_user_id = $1
		ORDER BY a.updated_at DESC, a.id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Appointment, 0)
	for rows.Next() {
		appt, err := scanAppointmentRead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, appt)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpdateAppointmentStatus(ctx context.Context, id ID, fromStatus, toStatus string, updatedAt time.Time) (Appointment, error) {
	if p == nil || p.db == nil {
		return Appointment{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		UPDATE verified.appointments
		SET status = $3, updated_at = $4
		WHERE id = $1 AND status = $2
		RETURNING id, listing_id, requester_user_id, provider_user_id, status, requested_at, scheduled_at, updated_at`,
		id, fromStatus, toStatus, updatedAt.UTC())
	appt, err := scanAppointment(row)
	if errors.Is(err, errNotFound) {
		existing, getErr := p.GetAppointment(ctx, id)
		if getErr != nil {
			return Appointment{}, getErr
		}
		if existing.Status != fromStatus {
			return Appointment{}, errInvalidTransition
		}
		return Appointment{}, errNotFound
	}
	return appt, err
}

func (p *PostgresStore) ReplaceChallenge(ctx context.Context, ch Challenge, now time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE verified.verification_challenges
		SET expires_at = $1
		WHERE consumed_at IS NULL
		  AND expires_at > $1
		  AND (
			($2::uuid IS NOT NULL AND appointment_id = $2)
			OR ($3::uuid IS NOT NULL AND flow_id = $3)
		  )`, now.UTC(), idOrNil(ch.AppointmentID), idOrNil(ch.FlowID)); err != nil {
		return mapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified.verification_challenges (
			id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ch.ID, idOrNil(ch.AppointmentID), idOrNil(ch.FlowID), ch.Method, ch.TokenHash, ch.ExpiresAt.UTC(), ch.ConsumedAt, ch.CreatedAt.UTC()); err != nil {
		return mapDBErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) CreateFlowWithChallenge(ctx context.Context, flow VerificationFlow, ch Challenge, now time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := flow.Validate(); err != nil {
		return err
	}
	if err := ch.Validate(); err != nil {
		return err
	}
	if ch.FlowID != flow.ID || !ch.AppointmentID.IsZero() {
		return errInvalidChallenge
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified.verification_flows (
			id, listing_id, requester_user_id, provider_user_id, interaction_type, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		flow.ID, flow.ListingID, flow.RequesterUserID, flow.ProviderUserID,
		flow.InteractionType, flow.Status, flow.CreatedAt.UTC(), flow.UpdatedAt.UTC()); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return errConflict
		}
		return mapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified.verification_challenges (
			id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ch.ID, idOrNil(ch.AppointmentID), idOrNil(ch.FlowID), ch.Method, ch.TokenHash, ch.ExpiresAt.UTC(), ch.ConsumedAt, ch.CreatedAt.UTC()); err != nil {
		return mapDBErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) GetFlow(ctx context.Context, id ID) (VerificationFlow, error) {
	if p == nil || p.db == nil {
		return VerificationFlow{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT f.id, f.listing_id, f.requester_user_id, f.provider_user_id, f.interaction_type,
			f.status, f.created_at, f.updated_at, i.id
		FROM verified.verification_flows f
		LEFT JOIN verified.verified_interactions i ON i.flow_id = f.id
		WHERE f.id = $1`, id)
	return scanFlowRead(row)
}

func (p *PostgresStore) ListFlowsForUser(ctx context.Context, userID ID, listingID ID, limit int) ([]VerificationFlow, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = MaxVerificationFlows
	}
	rows, err := p.db.Query(ctx, `
		SELECT f.id, f.listing_id, f.requester_user_id, f.provider_user_id, f.interaction_type,
			f.status, f.created_at, f.updated_at, i.id
		FROM verified.verification_flows f
		LEFT JOIN verified.verified_interactions i ON i.flow_id = f.id
		WHERE (f.requester_user_id = $1 OR f.provider_user_id = $1)
		  AND ($2::uuid IS NULL OR f.listing_id = $2)
		ORDER BY f.created_at DESC, f.id DESC
		LIMIT $3`, userID, idOrNil(listingID), limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]VerificationFlow, 0)
	for rows.Next() {
		flow, err := scanFlowRead(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, flow)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) GetChallenge(ctx context.Context, id ID) (Challenge, error) {
	if p == nil || p.db == nil {
		return Challenge{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		FROM verified.verification_challenges
		WHERE id = $1`, id)
	return scanChallenge(row)
}

func (p *PostgresStore) ListChallenges(ctx context.Context, appointmentID ID) ([]Challenge, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		FROM verified.verification_challenges
		WHERE appointment_id = $1
		ORDER BY created_at DESC, id DESC`, appointmentID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Challenge, 0)
	for rows.Next() {
		ch, err := scanChallenge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) GetInteraction(ctx context.Context, id ID) (VerifiedInteraction, error) {
	if p == nil || p.db == nil {
		return VerifiedInteraction{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, appointment_id, flow_id, listing_id, requester_user_id, provider_user_id,
			interaction_type, verification_method, verified_at
		FROM verified.verified_interactions
		WHERE id = $1`, id)
	return scanInteraction(row)
}

func (p *PostgresStore) GetInteractionByAppointment(ctx context.Context, appointmentID ID) (VerifiedInteraction, error) {
	if p == nil || p.db == nil {
		return VerifiedInteraction{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, appointment_id, flow_id, listing_id, requester_user_id, provider_user_id,
			interaction_type, verification_method, verified_at
		FROM verified.verified_interactions
		WHERE appointment_id = $1`, appointmentID)
	return scanInteraction(row)
}

func (p *PostgresStore) CompleteVerification(ctx context.Context, appointmentID ID, tokenHash []byte, interaction VerifiedInteraction, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error) {
	if p == nil || p.db == nil {
		return VerifiedInteraction{}, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	appt, err := scanAppointment(tx.QueryRow(ctx, `
		SELECT id, listing_id, requester_user_id, provider_user_id, status, requested_at, scheduled_at, updated_at
		FROM verified.appointments
		WHERE id = $1
		FOR UPDATE`, appointmentID))
	if err != nil {
		return VerifiedInteraction{}, err
	}
	if appt.Status != StatusAccepted {
		return VerifiedInteraction{}, errInvalidTransition
	}
	ch, err := scanChallenge(tx.QueryRow(ctx, `
		SELECT id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		FROM verified.verification_challenges
		WHERE appointment_id = $1 AND token_hash = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE`, appointmentID, tokenHash))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerifiedInteraction{}, errInvalidToken
		}
		return VerifiedInteraction{}, err
	}
	if ch.Consumed() {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if ch.Expired(now) {
		return VerifiedInteraction{}, errExpiredChallenge
	}
	if !bytes.Equal(ch.TokenHash, tokenHash) {
		return VerifiedInteraction{}, errInvalidToken
	}
	consumed := now.UTC()
	n, err := tx.Exec(ctx, `
		UPDATE verified.verification_challenges
		SET consumed_at = $2
		WHERE id = $1 AND consumed_at IS NULL`, ch.ID, consumed)
	if err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	if n == 0 {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if n, err := tx.Exec(ctx, `
		UPDATE verified.appointments
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = $4`, appointmentID, StatusCompleted, now.UTC(), StatusAccepted); err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	} else if n == 0 {
		return VerifiedInteraction{}, errInvalidTransition
	}
	interaction.AppointmentID = appointmentID
	interaction.FlowID = ID{}
	interaction.InteractionType = InteractionListingInspection
	interaction.VerificationMethod = ch.Method
	interaction.VerifiedAt = now.UTC()
	if err := interaction.Validate(); err != nil {
		return VerifiedInteraction{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified.verified_interactions (
			id, appointment_id, flow_id, listing_id, requester_user_id, provider_user_id,
			interaction_type, verification_method, verified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		interaction.ID, idOrNil(interaction.AppointmentID), idOrNil(interaction.FlowID), interaction.ListingID,
		interaction.RequesterUserID, interaction.ProviderUserID,
		interaction.InteractionType, interaction.VerificationMethod, interaction.VerifiedAt); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return VerifiedInteraction{}, errConflict
		}
		return VerifiedInteraction{}, mapDBErr(err)
	}
	if enqueue != nil {
		if err := enqueue(ctx, tx, interaction); err != nil {
			return VerifiedInteraction{}, mapDBErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	return interaction, nil
}

func (p *PostgresStore) CompleteFlowVerification(ctx context.Context, flowID ID, tokenHash []byte, interactionID ID, now time.Time, enqueue func(ctx context.Context, exec outbox.Execer, completed VerifiedInteraction) error) (VerifiedInteraction, error) {
	if p == nil || p.db == nil {
		return VerifiedInteraction{}, errUnavailable
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	flow, err := scanFlow(tx.QueryRow(ctx, `
		SELECT id, listing_id, requester_user_id, provider_user_id, interaction_type, status, created_at, updated_at
		FROM verified.verification_flows
		WHERE id = $1
		FOR UPDATE`, flowID))
	if err != nil {
		return VerifiedInteraction{}, err
	}
	if flow.Status != FlowOpen {
		return VerifiedInteraction{}, errInvalidTransition
	}
	ch, err := scanChallenge(tx.QueryRow(ctx, `
		SELECT id, appointment_id, flow_id, method, token_hash, expires_at, consumed_at, created_at
		FROM verified.verification_challenges
		WHERE flow_id = $1 AND token_hash = $2
		ORDER BY created_at DESC, id DESC
		LIMIT 1
		FOR UPDATE`, flowID, tokenHash))
	if err != nil {
		if errors.Is(err, errNotFound) {
			return VerifiedInteraction{}, errInvalidToken
		}
		return VerifiedInteraction{}, err
	}
	if ch.Consumed() {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if ch.Expired(now) {
		return VerifiedInteraction{}, errExpiredChallenge
	}
	if !bytes.Equal(ch.TokenHash, tokenHash) {
		return VerifiedInteraction{}, errInvalidToken
	}
	consumed := now.UTC()
	n, err := tx.Exec(ctx, `
		UPDATE verified.verification_challenges
		SET consumed_at = $2
		WHERE id = $1 AND consumed_at IS NULL`, ch.ID, consumed)
	if err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	if n == 0 {
		return VerifiedInteraction{}, errChallengeConsumed
	}
	if n, err := tx.Exec(ctx, `
		UPDATE verified.verification_flows
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = $4`, flowID, FlowCompleted, now.UTC(), FlowOpen); err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	} else if n == 0 {
		return VerifiedInteraction{}, errInvalidTransition
	}
	interaction := VerifiedInteraction{
		ID:                 interactionID,
		FlowID:             flow.ID,
		ListingID:          flow.ListingID,
		RequesterUserID:    flow.RequesterUserID,
		ProviderUserID:     flow.ProviderUserID,
		InteractionType:    flow.InteractionType,
		VerificationMethod: ch.Method,
		VerifiedAt:         now.UTC(),
	}
	if err := interaction.Validate(); err != nil {
		return VerifiedInteraction{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO verified.verified_interactions (
			id, appointment_id, flow_id, listing_id, requester_user_id, provider_user_id,
			interaction_type, verification_method, verified_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		interaction.ID, idOrNil(interaction.AppointmentID), idOrNil(interaction.FlowID), interaction.ListingID,
		interaction.RequesterUserID, interaction.ProviderUserID,
		interaction.InteractionType, interaction.VerificationMethod, interaction.VerifiedAt); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return VerifiedInteraction{}, errConflict
		}
		return VerifiedInteraction{}, mapDBErr(err)
	}
	if enqueue != nil {
		if err := enqueue(ctx, tx, interaction); err != nil {
			return VerifiedInteraction{}, mapDBErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	return interaction, nil
}

func scanAppointment(row interface {
	Scan(dest ...any) error
}) (Appointment, error) {
	var appt Appointment
	if err := row.Scan(
		&appt.ID, &appt.ListingID, &appt.RequesterUserID, &appt.ProviderUserID,
		&appt.Status, &appt.RequestedAt, &appt.ScheduledAt, &appt.UpdatedAt,
	); err != nil {
		return Appointment{}, mapDBErr(err)
	}
	return appt, nil
}

func scanAppointmentRead(row interface {
	Scan(dest ...any) error
}) (Appointment, error) {
	var appt Appointment
	var interactionID pgtype.UUID
	if err := row.Scan(
		&appt.ID, &appt.ListingID, &appt.RequesterUserID, &appt.ProviderUserID,
		&appt.Status, &appt.RequestedAt, &appt.ScheduledAt, &appt.UpdatedAt,
		&interactionID,
	); err != nil {
		return Appointment{}, mapDBErr(err)
	}
	if interactionID.Valid {
		var id ID
		copy(id[:], interactionID.Bytes[:])
		if !id.IsZero() {
			appt.VerifiedInteractionID = &id
		}
	}
	return appt, nil
}

func scanChallenge(row interface {
	Scan(dest ...any) error
}) (Challenge, error) {
	var ch Challenge
	var appointmentID, flowID pgtype.UUID
	if err := row.Scan(
		&ch.ID, &appointmentID, &flowID, &ch.Method, &ch.TokenHash, &ch.ExpiresAt, &ch.ConsumedAt, &ch.CreatedAt,
	); err != nil {
		return Challenge{}, mapDBErr(err)
	}
	ch.AppointmentID = uuidToID(appointmentID)
	ch.FlowID = uuidToID(flowID)
	return ch, nil
}

func scanInteraction(row interface {
	Scan(dest ...any) error
}) (VerifiedInteraction, error) {
	var v VerifiedInteraction
	var appointmentID, flowID pgtype.UUID
	if err := row.Scan(
		&v.ID, &appointmentID, &flowID, &v.ListingID, &v.RequesterUserID, &v.ProviderUserID,
		&v.InteractionType, &v.VerificationMethod, &v.VerifiedAt,
	); err != nil {
		return VerifiedInteraction{}, mapDBErr(err)
	}
	v.AppointmentID = uuidToID(appointmentID)
	v.FlowID = uuidToID(flowID)
	return v, nil
}

func scanFlow(row interface {
	Scan(dest ...any) error
}) (VerificationFlow, error) {
	var f VerificationFlow
	if err := row.Scan(
		&f.ID, &f.ListingID, &f.RequesterUserID, &f.ProviderUserID,
		&f.InteractionType, &f.Status, &f.CreatedAt, &f.UpdatedAt,
	); err != nil {
		return VerificationFlow{}, mapDBErr(err)
	}
	return f, nil
}

func scanFlowRead(row interface {
	Scan(dest ...any) error
}) (VerificationFlow, error) {
	var f VerificationFlow
	var interactionID pgtype.UUID
	if err := row.Scan(
		&f.ID, &f.ListingID, &f.RequesterUserID, &f.ProviderUserID,
		&f.InteractionType, &f.Status, &f.CreatedAt, &f.UpdatedAt,
		&interactionID,
	); err != nil {
		return VerificationFlow{}, mapDBErr(err)
	}
	if interactionID.Valid {
		var id ID
		copy(id[:], interactionID.Bytes[:])
		if !id.IsZero() {
			f.CompletedInteractionID = &id
		}
	}
	return f, nil
}

func idOrNil(id ID) any {
	if id.IsZero() {
		return nil
	}
	return id
}

func uuidToID(u pgtype.UUID) ID {
	if !u.Valid {
		return ID{}
	}
	var id ID
	copy(id[:], u.Bytes[:])
	return id
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
