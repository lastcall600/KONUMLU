package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"backend/internal/notifications/policy"
	"backend/internal/platform/db"
)

type sqlQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (int64, error)
	QueryRow(ctx context.Context, sql string, args ...any) db.Row
	Query(ctx context.Context, sql string, args ...any) (db.Rows, error)
}

func mapPolicyDBErr(err error) error {
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

var errConflict = errors.New("notification conflict")

const consentSelectCols = `id, user_id, consent_type, decision, policy_version, source, recorded_at, recorded_seq`

func (p *PostgresStore) ListPreferenceSettings(ctx context.Context, userID ID) ([]policy.PreferenceSetting, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	rows, err := p.db.Query(ctx, `
		SELECT channel, scope_type, scope_key, enabled
		FROM notifications.preference_settings
		WHERE user_id = $1
		ORDER BY channel, scope_type, scope_key`, userID)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]policy.PreferenceSetting, 0)
	for rows.Next() {
		var ch, st, key string
		var enabled bool
		if err := rows.Scan(&ch, &st, &key, &enabled); err != nil {
			return nil, mapPolicyDBErr(err)
		}
		out = append(out, policy.PreferenceSetting{
			Channel: policy.Channel(ch), ScopeType: policy.ScopeType(st), ScopeKey: key, Enabled: enabled,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpsertPreferenceSetting(ctx context.Context, userID ID, setting policy.PreferenceSetting, now time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if userID.IsZero() {
		return policy.ErrInvalidActor
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO notifications.preference_settings (
			user_id, channel, scope_type, scope_key, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$6)
		ON CONFLICT (user_id, channel, scope_type, scope_key) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at`,
		userID, string(setting.Channel), string(setting.ScopeType), setting.ScopeKey, setting.Enabled, now)
	return mapPolicyDBErr(err)
}

func scanConsent(row scanner) (policy.ConsentSnapshot, ID, error) {
	var id, userID ID
	var typ, decision, version, source string
	var recordedAt time.Time
	var seq int64
	if err := row.Scan(&id, &userID, &typ, &decision, &version, &source, &recordedAt, &seq); err != nil {
		return policy.ConsentSnapshot{}, ID{}, mapPolicyDBErr(err)
	}
	snap := policy.ConsentSnapshot{
		ID:            id.String(),
		Type:          policy.ConsentType(typ),
		Status:        policy.ConsentStatus(decision),
		PolicyVersion: version,
		CapturedAt:    recordedAt.UTC(),
		Source:        policy.ConsentSource(source),
		RecordedSeq:   seq,
	}
	if snap.Status == policy.ConsentWithdrawn {
		t := recordedAt.UTC()
		snap.WithdrawnAt = &t
	}
	return snap, userID, nil
}

func (p *PostgresStore) InsertConsentDecision(ctx context.Context, userID ID, snap policy.ConsentSnapshot) (policy.ConsentSnapshot, error) {
	if p == nil || p.db == nil {
		return policy.ConsentSnapshot{}, errUnavailable
	}
	if userID.IsZero() {
		return policy.ConsentSnapshot{}, policy.ErrInvalidActor
	}
	id, err := NewID()
	if err != nil {
		return policy.ConsentSnapshot{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		INSERT INTO notifications.consent_decisions (
			id, user_id, consent_type, decision, policy_version, source, recorded_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING `+consentSelectCols,
		id, userID, string(snap.Type), string(snap.Status), snap.PolicyVersion, string(snap.Source), snap.CapturedAt.UTC())
	got, _, err := scanConsent(row)
	if err != nil {
		return policy.ConsentSnapshot{}, err
	}
	return got, nil
}

func (p *PostgresStore) CurrentConsent(ctx context.Context, userID ID, typ policy.ConsentType) (policy.ConsentSnapshot, error) {
	if p == nil || p.db == nil {
		return policy.ConsentSnapshot{}, errUnavailable
	}
	if userID.IsZero() {
		return policy.ConsentSnapshot{}, policy.ErrInvalidActor
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+consentSelectCols+`
		FROM notifications.consent_decisions
		WHERE user_id = $1 AND consent_type = $2
		ORDER BY recorded_seq DESC
		LIMIT 1`, userID, string(typ))
	got, _, err := scanConsent(row)
	if err != nil {
		return policy.ConsentSnapshot{}, err
	}
	return got, nil
}

func (p *PostgresStore) ListConsentHistory(ctx context.Context, userID ID, typ policy.ConsentType) ([]policy.ConsentSnapshot, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+consentSelectCols+`
		FROM notifications.consent_decisions
		WHERE user_id = $1 AND consent_type = $2
		ORDER BY recorded_seq ASC`, userID, string(typ))
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]policy.ConsentSnapshot, 0)
	for rows.Next() {
		snap, _, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) ListCurrentConsents(ctx context.Context, userID ID) ([]policy.ConsentSnapshot, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	rows, err := p.db.Query(ctx, `
		SELECT DISTINCT ON (consent_type) `+consentSelectCols+`
		FROM notifications.consent_decisions
		WHERE user_id = $1
		ORDER BY consent_type, recorded_seq DESC`, userID)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]policy.ConsentSnapshot, 0)
	for rows.Next() {
		snap, _, err := scanConsent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

type SemanticIntent struct {
	ID              ID
	RecipientUserID ID
	EventType       policy.EventType
	Purpose         policy.Purpose
	CatalogVersion  int
	TemplateKey     string
	Urgency         policy.Urgency
	LocaleHint      *string
	DomainRef       string
	ActorRef        *string
	ResourceRef     *string
	DedupeKey       string
	Variables       map[string]string
	CreatedAt       time.Time
}

type ChannelDeliveryRow struct {
	ID                ID
	IntentID          ID
	Channel           policy.Channel
	State             policy.DeliveryState
	Attempts          int
	NextAttemptAt     *time.Time
	SuppressionReason *policy.SuppressionReason
	LastErrorClass    *string
	ProviderRef       *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
}

type InboxRow struct {
	ID          ID
	IntentID    ID
	UserID      ID
	CreatedAt   time.Time
	ReadAt      *time.Time
	EventType   policy.EventType
	Purpose     policy.Purpose
	TemplateKey string
	Variables   map[string]string
	ResourceRef string
}

const intentSelectCols = `id, recipient_user_id, event_type, purpose, catalog_version, template_key, urgency,
	locale_hint, domain_ref, actor_ref, resource_ref, dedupe_key, variables, created_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanIntent(row scanner) (SemanticIntent, error) {
	var in SemanticIntent
	var vars []byte
	if err := row.Scan(
		&in.ID, &in.RecipientUserID, &in.EventType, &in.Purpose, &in.CatalogVersion, &in.TemplateKey, &in.Urgency,
		&in.LocaleHint, &in.DomainRef, &in.ActorRef, &in.ResourceRef, &in.DedupeKey, &vars, &in.CreatedAt,
	); err != nil {
		return SemanticIntent{}, mapPolicyDBErr(err)
	}
	if len(vars) > 0 {
		_ = json.Unmarshal(vars, &in.Variables)
	}
	if in.Variables == nil {
		in.Variables = map[string]string{}
	}
	return in, nil
}

func (p *PostgresStore) insertIntentTx(ctx context.Context, q sqlQuerier, in SemanticIntent) (SemanticIntent, bool, error) {
	raw, err := json.Marshal(in.Variables)
	if err != nil {
		return SemanticIntent{}, false, errUnavailable
	}
	row := q.QueryRow(ctx, `
		INSERT INTO notifications.intents (
			id, recipient_user_id, event_type, purpose, catalog_version, template_key, urgency,
			locale_hint, domain_ref, actor_ref, resource_ref, dedupe_key, variables, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (recipient_user_id, dedupe_key) DO NOTHING
		RETURNING `+intentSelectCols,
		in.ID, in.RecipientUserID, string(in.EventType), string(in.Purpose), in.CatalogVersion, in.TemplateKey, string(in.Urgency),
		in.LocaleHint, in.DomainRef, in.ActorRef, in.ResourceRef, in.DedupeKey, raw, in.CreatedAt.UTC())
	got, err := scanIntent(row)
	if err == nil {
		return got, true, nil
	}
	if !errors.Is(err, errNotFound) {
		return SemanticIntent{}, false, err
	}
	existing, err := getIntentByDedupeTx(ctx, q, in.RecipientUserID, in.DedupeKey)
	if err != nil {
		return SemanticIntent{}, false, err
	}
	return existing, false, nil
}

func getIntentByDedupeTx(ctx context.Context, q sqlQuerier, userID ID, dedupe string) (SemanticIntent, error) {
	row := q.QueryRow(ctx, `
		SELECT `+intentSelectCols+`
		FROM notifications.intents
		WHERE recipient_user_id = $1 AND dedupe_key = $2`, userID, dedupe)
	return scanIntent(row)
}

func (p *PostgresStore) insertChannelDeliveryTx(ctx context.Context, q sqlQuerier, row ChannelDeliveryRow) (ChannelDeliveryRow, error) {
	var reason *string
	if row.SuppressionReason != nil {
		s := string(*row.SuppressionReason)
		reason = &s
	}
	scanned := q.QueryRow(ctx, `
		INSERT INTO notifications.channel_deliveries (
			id, intent_id, channel, state, attempts, next_attempt_at, suppression_reason, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (intent_id, channel) DO NOTHING
		RETURNING id, intent_id, channel, state, attempts, suppression_reason, created_at, updated_at`,
		row.ID, row.IntentID, string(row.Channel), string(row.State), row.Attempts, row.NextAttemptAt, reason, row.CreatedAt.UTC(), row.UpdatedAt.UTC())
	got, err := scanChannelDelivery(scanned)
	if err == nil {
		return got, nil
	}
	if !errors.Is(err, errNotFound) {
		return ChannelDeliveryRow{}, err
	}
	return getChannelDeliveryTx(ctx, q, row.IntentID, row.Channel)
}

func scanChannelDelivery(row scanner) (ChannelDeliveryRow, error) {
	var d ChannelDeliveryRow
	var ch, state string
	var reason *string
	if err := row.Scan(&d.ID, &d.IntentID, &ch, &state, &d.Attempts, &reason, &d.CreatedAt, &d.UpdatedAt); err != nil {
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

func getChannelDeliveryTx(ctx context.Context, q sqlQuerier, intentID ID, ch policy.Channel) (ChannelDeliveryRow, error) {
	row := q.QueryRow(ctx, `
		SELECT id, intent_id, channel, state, attempts, suppression_reason, created_at, updated_at
		FROM notifications.channel_deliveries
		WHERE intent_id = $1 AND channel = $2`, intentID, string(ch))
	return scanChannelDelivery(row)
}

func (p *PostgresStore) insertInboxTx(ctx context.Context, q sqlQuerier, item InboxRow) (InboxRow, error) {
	scanned := q.QueryRow(ctx, `
		INSERT INTO notifications.inbox_items (id, intent_id, user_id, created_at, read_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (intent_id) DO NOTHING
		RETURNING id, intent_id, user_id, created_at, read_at`,
		item.ID, item.IntentID, item.UserID, item.CreatedAt.UTC(), item.ReadAt)
	got, err := scanInboxBare(scanned)
	if err == nil {
		return got, nil
	}
	if !errors.Is(err, errNotFound) {
		return InboxRow{}, err
	}
	return getInboxByIntentTx(ctx, q, item.IntentID)
}

func scanInboxBare(row scanner) (InboxRow, error) {
	var item InboxRow
	if err := row.Scan(&item.ID, &item.IntentID, &item.UserID, &item.CreatedAt, &item.ReadAt); err != nil {
		return InboxRow{}, mapPolicyDBErr(err)
	}
	return item, nil
}

func getInboxByIntentTx(ctx context.Context, q sqlQuerier, intentID ID) (InboxRow, error) {
	row := q.QueryRow(ctx, `
		SELECT id, intent_id, user_id, created_at, read_at
		FROM notifications.inbox_items WHERE intent_id = $1`, intentID)
	return scanInboxBare(row)
}

func (p *PostgresStore) ListInbox(ctx context.Context, userID ID, cursor *inboxCursor, limit int) ([]InboxRow, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if userID.IsZero() {
		return nil, policy.ErrInvalidActor
	}
	sql := `
		SELECT i.id, i.intent_id, i.user_id, i.created_at, i.read_at,
			n.event_type, n.purpose, n.template_key, n.variables, COALESCE(n.resource_ref, '')
		FROM notifications.inbox_items i
		JOIN notifications.intents n ON n.id = i.intent_id
		WHERE i.user_id = $1`
	args := []any{userID}
	if cursor != nil {
		sql += ` AND (i.created_at, i.id) < ($2, $3)`
		args = append(args, cursor.CreatedAt.UTC(), cursor.ID)
	}
	sql += ` ORDER BY i.created_at DESC, i.id DESC LIMIT $` + itoa(len(args)+1)
	args = append(args, limit)
	rows, err := p.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]InboxRow, 0)
	for rows.Next() {
		item, err := scanInboxJoined(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func scanInboxJoined(row scanner) (InboxRow, error) {
	var item InboxRow
	var vars []byte
	if err := row.Scan(
		&item.ID, &item.IntentID, &item.UserID, &item.CreatedAt, &item.ReadAt,
		&item.EventType, &item.Purpose, &item.TemplateKey, &vars, &item.ResourceRef,
	); err != nil {
		return InboxRow{}, mapPolicyDBErr(err)
	}
	if len(vars) > 0 {
		_ = json.Unmarshal(vars, &item.Variables)
	}
	if item.Variables == nil {
		item.Variables = map[string]string{}
	}
	return item, nil
}

func (p *PostgresStore) GetInboxOwned(ctx context.Context, userID, inboxID ID) (InboxRow, error) {
	if p == nil || p.db == nil {
		return InboxRow{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT i.id, i.intent_id, i.user_id, i.created_at, i.read_at,
			n.event_type, n.purpose, n.template_key, n.variables, COALESCE(n.resource_ref, '')
		FROM notifications.inbox_items i
		JOIN notifications.intents n ON n.id = i.intent_id
		WHERE i.id = $1 AND i.user_id = $2`, inboxID, userID)
	return scanInboxJoined(row)
}

func (p *PostgresStore) MarkInboxRead(ctx context.Context, userID, inboxID ID, now time.Time) (InboxRow, error) {
	if p == nil || p.db == nil {
		return InboxRow{}, errUnavailable
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	n, err := p.db.Exec(ctx, `
		UPDATE notifications.inbox_items
		SET read_at = COALESCE(read_at, $3)
		WHERE id = $1 AND user_id = $2`, inboxID, userID, now)
	if err != nil {
		return InboxRow{}, mapPolicyDBErr(err)
	}
	if n == 0 {
		return InboxRow{}, errNotFound
	}
	return p.GetInboxOwned(ctx, userID, inboxID)
}

func (p *PostgresStore) MarkAllInboxRead(ctx context.Context, userID ID, now time.Time) (int64, error) {
	if p == nil || p.db == nil {
		return 0, errUnavailable
	}
	if userID.IsZero() {
		return 0, policy.ErrInvalidActor
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	n, err := p.db.Exec(ctx, `
		UPDATE notifications.inbox_items
		SET read_at = $2
		WHERE user_id = $1 AND read_at IS NULL`, userID, now)
	if err != nil {
		return 0, mapPolicyDBErr(err)
	}
	return n, nil
}

func (p *PostgresStore) CountUnreadInbox(ctx context.Context, userID ID) (int64, error) {
	if p == nil || p.db == nil {
		return 0, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications.inbox_items
		WHERE user_id = $1 AND read_at IS NULL`, userID)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, mapPolicyDBErr(err)
	}
	return n, nil
}

func (p *PostgresStore) GetIntentByDedupe(ctx context.Context, userID ID, dedupe string) (SemanticIntent, error) {
	if p == nil || p.db == nil {
		return SemanticIntent{}, errUnavailable
	}
	return getIntentByDedupeTx(ctx, p.db, userID, dedupe)
}

func (p *PostgresStore) ListChannelDeliveries(ctx context.Context, intentID ID) ([]ChannelDeliveryRow, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+channelDeliverySelect+`
		FROM notifications.channel_deliveries WHERE intent_id = $1 ORDER BY channel`, intentID)
	if err != nil {
		return nil, mapPolicyDBErr(err)
	}
	defer rows.Close()
	out := make([]ChannelDeliveryRow, 0)
	for rows.Next() {
		d, err := scanChannelDeliveryFull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapPolicyDBErr(err)
	}
	return out, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func nullableString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
