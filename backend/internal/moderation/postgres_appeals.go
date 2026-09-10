package moderation

import (
	"context"
	"time"
)

func (p *PostgresStore) InsertAppeal(ctx context.Context, row Appeal, history CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if history.CaseID != row.CaseID || history.Kind != HistoryAppealSubmitted {
		return errInvalidHistory
	}
	if history.AppealID == nil || *history.AppealID != row.ID {
		return errInvalidHistory
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		if _, err := p.GetCase(ctx, row.CaseID); err != nil {
			return err
		}
		if _, err := p.GetAction(ctx, row.ActionID); err != nil {
			return err
		}
		_, err := p.db.Exec(ctx, `
			INSERT INTO moderation.appeals (
				id, action_id, case_id, appellant_user_id, statement, status,
				created_at, updated_at, decided_at, decided_by_staff_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			row.ID, row.ActionID, row.CaseID, row.AppellantUserID, row.Statement, string(row.Status),
			row.CreatedAt.UTC(), row.UpdatedAt.UTC(), timeOrNil(row.DecidedAt), uuidOrNil(row.DecidedByStaffID),
		)
		if err != nil {
			return mapDBErr(err)
		}
		return p.insertHistory(ctx, []CaseHistory{history})
	})
}

func (p *PostgresStore) GetAppeal(ctx context.Context, id ID) (Appeal, error) {
	if p == nil || p.db == nil {
		return Appeal{}, errUnavailable
	}
	if id.IsZero() {
		return Appeal{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+appealSelectCols+`
		FROM moderation.appeals
		WHERE id = $1`, id)
	return scanAppeal(row)
}

func (p *PostgresStore) FindActiveAppeal(ctx context.Context, actionID, appellantUserID ID) (Appeal, error) {
	if p == nil || p.db == nil {
		return Appeal{}, errUnavailable
	}
	if actionID.IsZero() || appellantUserID.IsZero() {
		return Appeal{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+appealSelectCols+`
		FROM moderation.appeals
		WHERE action_id = $1
			AND appellant_user_id = $2
			AND status IN ('submitted', 'under_review')
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, actionID, appellantUserID)
	return scanAppeal(row)
}

func (p *PostgresStore) ListAppeals(ctx context.Context, caseID ID, limit int) ([]Appeal, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if _, err := p.GetCase(ctx, caseID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = DefaultAppealLimit
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+appealSelectCols+`
		FROM moderation.appeals
		WHERE case_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2`, caseID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Appeal, 0)
	for rows.Next() {
		row, err := scanAppeal(rows)
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

func (p *PostgresStore) ListAppealsForAppellant(ctx context.Context, appellantUserID ID, limit int) ([]Appeal, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if appellantUserID.IsZero() {
		return nil, errZeroID
	}
	if limit <= 0 {
		limit = MaxMineAppeals
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+appealSelectCols+`
		FROM moderation.appeals
		WHERE appellant_user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, appellantUserID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Appeal, 0)
	for rows.Next() {
		row, err := scanAppeal(rows)
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

func (p *PostgresStore) UpdateAppeal(ctx context.Context, from, to Appeal, history []CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := to.Validate(); err != nil {
		return err
	}
	if len(history) == 0 {
		return errInvalidHistory
	}
	for _, h := range history {
		if err := h.Validate(); err != nil {
			return err
		}
		if from.ID != to.ID || h.CaseID != to.CaseID {
			return errInvalidAppeal
		}
		if h.AppealID == nil || *h.AppealID != to.ID {
			return errInvalidHistory
		}
	}
	if from.ActionID != to.ActionID || from.CaseID != to.CaseID ||
		from.AppellantUserID != to.AppellantUserID || from.Statement != to.Statement {
		return errInvalidAppeal
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		n, err := p.db.Exec(ctx, `
			UPDATE moderation.appeals
			SET status = $2,
				updated_at = $3,
				decided_at = $4,
				decided_by_staff_id = $5
			WHERE id = $1
				AND status = $6
				AND action_id = $7
				AND case_id = $8
				AND appellant_user_id = $9
				AND statement = $10`,
			to.ID, string(to.Status), to.UpdatedAt.UTC(), timeOrNil(to.DecidedAt), uuidOrNil(to.DecidedByStaffID),
			string(from.Status), from.ActionID, from.CaseID, from.AppellantUserID, from.Statement,
		)
		if err != nil {
			return mapDBErr(err)
		}
		if n == 0 {
			return errConflict
		}
		return p.insertHistory(ctx, history)
	})
}

const appealSelectCols = `id, action_id, case_id, appellant_user_id, statement, status, created_at, updated_at, decided_at, decided_by_staff_id`

func scanAppeal(row interface {
	Scan(dest ...any) error
}) (Appeal, error) {
	var a Appeal
	var status string
	if err := row.Scan(
		&a.ID, &a.ActionID, &a.CaseID, &a.AppellantUserID, &a.Statement, &status,
		&a.CreatedAt, &a.UpdatedAt, &a.DecidedAt, &a.DecidedByStaffID,
	); err != nil {
		return Appeal{}, mapDBErr(err)
	}
	a.Status = AppealStatus(status)
	return a, nil
}

func timeOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
