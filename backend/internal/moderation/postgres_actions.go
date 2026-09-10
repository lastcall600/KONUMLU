package moderation

import (
	"context"
)

func (p *PostgresStore) InsertAction(ctx context.Context, row CaseAction, history CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if history.CaseID != row.CaseID || history.Kind != HistoryActionProposed {
		return errInvalidHistory
	}
	if history.ActionID == nil || *history.ActionID != row.ID {
		return errInvalidHistory
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		if _, err := p.GetCase(ctx, row.CaseID); err != nil {
			return err
		}
		_, err := p.db.Exec(ctx, `
			INSERT INTO moderation.case_actions (
				id, case_id, target_type, target_id, action_type, status, reason_code,
				rationale, actor_staff_id, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			row.ID, row.CaseID, string(row.TargetType), row.TargetID, string(row.ActionType),
			string(row.Status), string(row.ReasonCode), row.Rationale, uuidOrNil(row.ActorStaffID),
			row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
		)
		if err != nil {
			return mapDBErr(err)
		}
		return p.insertHistory(ctx, []CaseHistory{history})
	})
}

func (p *PostgresStore) GetAction(ctx context.Context, id ID) (CaseAction, error) {
	if p == nil || p.db == nil {
		return CaseAction{}, errUnavailable
	}
	if id.IsZero() {
		return CaseAction{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, case_id, target_type, target_id, action_type, status, reason_code,
			rationale, actor_staff_id, created_at, updated_at
		FROM moderation.case_actions
		WHERE id = $1`, id)
	return scanAction(row)
}

func (p *PostgresStore) ListActions(ctx context.Context, caseID ID, limit int) ([]CaseAction, error) {
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
		limit = DefaultActionLimit
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, case_id, target_type, target_id, action_type, status, reason_code,
			rationale, actor_staff_id, created_at, updated_at
		FROM moderation.case_actions
		WHERE case_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2`, caseID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]CaseAction, 0)
	for rows.Next() {
		row, err := scanAction(rows)
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

func (p *PostgresStore) UpdateAction(ctx context.Context, from, to CaseAction, history CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := to.Validate(); err != nil {
		return err
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if from.ID != to.ID || history.CaseID != to.CaseID {
		return errInvalidAction
	}
	if history.ActionID == nil || *history.ActionID != to.ID {
		return errInvalidHistory
	}
	if from.TargetType != to.TargetType || from.TargetID != to.TargetID ||
		from.ActionType != to.ActionType || from.ReasonCode != to.ReasonCode {
		return errInvalidAction
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		n, err := p.db.Exec(ctx, `
			UPDATE moderation.case_actions
			SET status = $2,
				actor_staff_id = $3,
				updated_at = $4
			WHERE id = $1
				AND status = $5
				AND target_type = $6
				AND target_id = $7
				AND action_type = $8
				AND reason_code = $9`,
			to.ID, string(to.Status), uuidOrNil(to.ActorStaffID), to.UpdatedAt.UTC(),
			string(from.Status), string(from.TargetType), from.TargetID, string(from.ActionType),
			string(from.ReasonCode),
		)
		if err != nil {
			return mapDBErr(err)
		}
		if n == 0 {
			return errConflict
		}
		return p.insertHistory(ctx, []CaseHistory{history})
	})
}

func scanAction(row interface {
	Scan(dest ...any) error
}) (CaseAction, error) {
	var a CaseAction
	var targetType, actionType, status, reason string
	if err := row.Scan(
		&a.ID, &a.CaseID, &targetType, &a.TargetID, &actionType, &status, &reason,
		&a.Rationale, &a.ActorStaffID, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return CaseAction{}, mapDBErr(err)
	}
	a.TargetType = TargetType(targetType)
	a.ActionType = ActionType(actionType)
	a.Status = ActionStatus(status)
	a.ReasonCode = ActionReasonCode(reason)
	return a, nil
}
