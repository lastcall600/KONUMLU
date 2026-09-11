package moderation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/platform/db"
)

var (
	_ reportStore = (*PostgresStore)(nil)
	_ caseStore   = (*PostgresStore)(nil)
	_ persistence = (*PostgresStore)(nil)
)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

const reportSelectCols = `id, reporter_user_id, target_type, target_id, reason_code, description, status, staff_note, status_changed_by, created_at, updated_at`

func (p *PostgresStore) Insert(ctx context.Context, row Report) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	_, err := p.db.Exec(ctx, `
		INSERT INTO moderation.reports (
			id, reporter_user_id, target_type, target_id, reason_code,
			description, status, staff_note, status_changed_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		row.ID, row.ReporterUserID, string(row.TargetType), row.TargetID, string(row.ReasonCode),
		row.Description, string(row.Status), row.StaffNote, uuidOrNil(row.StatusChangedBy), row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
	)
	if err != nil {
		if errors.Is(err, db.ErrConflict) {
			return errConflict
		}
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) GetByID(ctx context.Context, id ID) (Report, error) {
	if p == nil || p.db == nil {
		return Report{}, errUnavailable
	}
	if id.IsZero() {
		return Report{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+reportSelectCols+`
		FROM moderation.reports
		WHERE id = $1`, id)
	return scanReport(row)
}

func (p *PostgresStore) FindRecentIdentical(ctx context.Context, reporterUserID ID, targetType TargetType, targetID ID, reason ReasonCode, since time.Time) (Report, error) {
	if p == nil || p.db == nil {
		return Report{}, errUnavailable
	}
	if reporterUserID.IsZero() || targetID.IsZero() {
		return Report{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+reportSelectCols+`
		FROM moderation.reports
		WHERE reporter_user_id = $1
			AND target_type = $2
			AND target_id = $3
			AND reason_code = $4
			AND created_at >= $5
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, reporterUserID, string(targetType), targetID, string(reason), since.UTC())
	return scanReport(row)
}

func (p *PostgresStore) ListForReporter(ctx context.Context, reporterUserID ID, limit int) ([]Report, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if reporterUserID.IsZero() {
		return nil, errZeroID
	}
	if limit <= 0 {
		limit = MaxMineReports
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+reportSelectCols+`
		FROM moderation.reports
		WHERE reporter_user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, reporterUserID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Report, 0)
	for rows.Next() {
		row, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) ListQueue(ctx context.Context, q QueueQuery, cursor *queueCursor, limit int) ([]Report, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = DefaultQueueLimit
	}
	var b strings.Builder
	b.WriteString(`SELECT ` + reportSelectCols + ` FROM moderation.reports WHERE TRUE`)
	args := make([]any, 0, 8)
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.Status != nil {
		b.WriteString(` AND status = ` + add(string(*q.Status)))
	}
	if q.TargetType != nil {
		b.WriteString(` AND target_type = ` + add(string(*q.TargetType)))
	}
	if q.TargetID != nil {
		b.WriteString(` AND target_id = ` + add(*q.TargetID))
	}
	if q.ReasonCode != nil {
		b.WriteString(` AND reason_code = ` + add(string(*q.ReasonCode)))
	}
	if cursor != nil {
		ts := add(cursor.CreatedAt.UTC())
		cid := add(cursor.ID)
		if q.Order == QueueOldest {
			b.WriteString(` AND (created_at > ` + ts + ` OR (created_at = ` + ts + ` AND id > ` + cid + `))`)
		} else {
			b.WriteString(` AND (created_at < ` + ts + ` OR (created_at = ` + ts + ` AND id < ` + cid + `))`)
		}
	}
	if q.Order == QueueOldest {
		b.WriteString(` ORDER BY created_at ASC, id ASC`)
	} else {
		b.WriteString(` ORDER BY created_at DESC, id DESC`)
	}
	b.WriteString(` LIMIT ` + add(limit))
	rows, err := p.db.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Report, 0)
	for rows.Next() {
		row, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) UpdateStatus(ctx context.Context, id ID, from, to Status, updatedAt time.Time, note *string, actor *ID) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if id.IsZero() {
		return errZeroID
	}
	n, err := p.db.Exec(ctx, `
		UPDATE moderation.reports
		SET status = $2,
			updated_at = $3,
			staff_note = CASE WHEN $4 THEN $5 ELSE staff_note END,
			status_changed_by = CASE WHEN $6 THEN $7 ELSE status_changed_by END
		WHERE id = $1 AND status = $8`,
		id, string(to), updatedAt.UTC(), note != nil, note, actor != nil, uuidOrNil(actor), string(from),
	)
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errConflict
	}
	return nil
}

func scanReport(row interface {
	Scan(dest ...any) error
}) (Report, error) {
	var r Report
	var targetType, reason, status string
	var changedBy *ID
	if err := row.Scan(
		&r.ID, &r.ReporterUserID, &targetType, &r.TargetID, &reason,
		&r.Description, &status, &r.StaffNote, &changedBy, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return Report{}, mapDBErr(err)
	}
	r.TargetType = TargetType(targetType)
	r.ReasonCode = ReasonCode(reason)
	r.Status = Status(status)
	r.StatusChangedBy = changedBy
	return r, nil
}

func uuidOrNil(id *ID) any {
	if id == nil || id.IsZero() {
		return nil
	}
	return *id
}

func (p *PostgresStore) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if fn == nil {
		return errUnavailable
	}
	return p.withTx(ctx, fn)
}

func (p *PostgresStore) withTx(ctx context.Context, fn func(context.Context) error) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if db.TxFrom(ctx) != nil {
		return fn(ctx)
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(db.WithTx(ctx, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
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
