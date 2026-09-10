package moderation

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const caseSelectCols = `id, status, priority, subject_type, subject_id, title, assigned_staff_id, created_at, updated_at`

const historySelectCols = `id, case_id, kind, actor_staff_id, report_id, from_status, to_status, from_priority, to_priority, from_assigned_staff_id, to_assigned_staff_id, note, evidence_id, action_id, appeal_id, created_at`

func (p *PostgresStore) InsertCase(ctx context.Context, row Case, reportIDs []ID, history []CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	for _, h := range history {
		if err := h.Validate(); err != nil {
			return err
		}
		if h.CaseID != row.ID {
			return errInvalidHistory
		}
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		if err := p.assertReportsAttachable(ctx, row, reportIDs); err != nil {
			return err
		}
		_, err := p.db.Exec(ctx, `
			INSERT INTO moderation.cases (
				id, status, priority, subject_type, subject_id, title,
				assigned_staff_id, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			row.ID, string(row.Status), string(row.Priority), string(row.SubjectType), row.SubjectID,
			row.Title, uuidOrNil(row.AssignedStaffID), row.CreatedAt.UTC(), row.UpdatedAt.UTC(),
		)
		if err != nil {
			return mapDBErr(err)
		}
		if err := p.insertCaseReports(ctx, row.ID, reportIDs, row.CreatedAt); err != nil {
			return err
		}
		return p.insertHistory(ctx, history)
	})
}

func (p *PostgresStore) GetCase(ctx context.Context, id ID) (Case, error) {
	if p == nil || p.db == nil {
		return Case{}, errUnavailable
	}
	if id.IsZero() {
		return Case{}, errZeroID
	}
	row := p.db.QueryRow(ctx, `
		SELECT `+caseSelectCols+`
		FROM moderation.cases
		WHERE id = $1`, id)
	return scanCase(row)
}

func (p *PostgresStore) ListCases(ctx context.Context, q CaseQuery, cursor *caseCursor, limit int) ([]Case, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = DefaultCaseLimit
	}
	var b strings.Builder
	b.WriteString(`SELECT ` + caseSelectCols + ` FROM moderation.cases WHERE TRUE`)
	args := make([]any, 0, 8)
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.Status != nil {
		b.WriteString(` AND status = ` + add(string(*q.Status)))
	}
	if q.Priority != nil {
		b.WriteString(` AND priority = ` + add(string(*q.Priority)))
	}
	if q.SubjectType != nil {
		b.WriteString(` AND subject_type = ` + add(string(*q.SubjectType)))
	}
	if cursor != nil {
		ts := add(cursor.CreatedAt.UTC())
		cid := add(cursor.ID)
		b.WriteString(` AND (created_at < ` + ts + ` OR (created_at = ` + ts + ` AND id < ` + cid + `))`)
	}
	b.WriteString(` ORDER BY created_at DESC, id DESC`)
	b.WriteString(` LIMIT ` + add(limit))
	rows, err := p.db.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Case, 0)
	for rows.Next() {
		row, err := scanCase(rows)
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

func (p *PostgresStore) ListCaseReportIDs(ctx context.Context, caseID ID) ([]ID, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if _, err := p.GetCase(ctx, caseID); err != nil {
		return nil, err
	}
	rows, err := p.db.Query(ctx, `
		SELECT report_id
		FROM moderation.case_reports
		WHERE case_id = $1
		ORDER BY attached_at ASC, report_id ASC`, caseID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]ID, 0)
	for rows.Next() {
		var id ID
		if err := rows.Scan(&id); err != nil {
			return nil, mapDBErr(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) ListCaseHistory(ctx context.Context, caseID ID) ([]CaseHistory, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if _, err := p.GetCase(ctx, caseID); err != nil {
		return nil, err
	}
	rows, err := p.db.Query(ctx, `
		SELECT `+historySelectCols+`
		FROM moderation.case_history
		WHERE case_id = $1
		ORDER BY created_at ASC, id ASC`, caseID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]CaseHistory, 0)
	for rows.Next() {
		row, err := scanHistory(rows)
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

func (p *PostgresStore) UpdateCase(ctx context.Context, from, to Case, history CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := to.Validate(); err != nil {
		return err
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if history.CaseID != to.ID || from.ID != to.ID {
		return errInvalidCase
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		n, err := p.db.Exec(ctx, `
			UPDATE moderation.cases
			SET status = $2,
				priority = $3,
				assigned_staff_id = $4,
				updated_at = $5
			WHERE id = $1
				AND status = $6
				AND priority = $7
				AND assigned_staff_id IS NOT DISTINCT FROM $8`,
			to.ID, string(to.Status), string(to.Priority), uuidOrNil(to.AssignedStaffID), to.UpdatedAt.UTC(),
			string(from.Status), string(from.Priority), uuidOrNil(from.AssignedStaffID),
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

func (p *PostgresStore) AttachCaseReports(ctx context.Context, caseID ID, reportIDs []ID, updatedAt time.Time, history []CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if caseID.IsZero() {
		return errZeroID
	}
	if len(reportIDs) == 0 {
		return errInvalidAttachment
	}
	for _, h := range history {
		if err := h.Validate(); err != nil {
			return err
		}
		if h.CaseID != caseID {
			return errInvalidHistory
		}
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		row, err := p.GetCase(ctx, caseID)
		if err != nil {
			return err
		}
		if !row.Status.IsActive() {
			return errInvalidAttachment
		}
		if err := p.assertReportsAttachable(ctx, row, reportIDs); err != nil {
			return err
		}
		if err := p.insertCaseReports(ctx, caseID, reportIDs, updatedAt); err != nil {
			return err
		}
		if _, err := p.db.Exec(ctx, `
			UPDATE moderation.cases SET updated_at = $2 WHERE id = $1`, caseID, updatedAt.UTC()); err != nil {
			return mapDBErr(err)
		}
		return p.insertHistory(ctx, history)
	})
}

func (p *PostgresStore) InsertEvidence(ctx context.Context, row CaseEvidence, history CaseHistory) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := row.Validate(); err != nil {
		return err
	}
	if err := history.Validate(); err != nil {
		return err
	}
	if history.CaseID != row.CaseID || history.Kind != HistoryEvidenceAdded {
		return errInvalidHistory
	}
	if history.EvidenceID == nil || *history.EvidenceID != row.ID {
		return errInvalidHistory
	}
	return p.withTx(ctx, func(ctx context.Context) error {
		if _, err := p.GetCase(ctx, row.CaseID); err != nil {
			return err
		}
		_, err := p.db.Exec(ctx, `
			INSERT INTO moderation.case_evidence (
				id, case_id, evidence_type, title, description, reference_value,
				actor_staff_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			row.ID, row.CaseID, string(row.EvidenceType), row.Title, row.Description, row.ReferenceValue,
			uuidOrNil(row.ActorStaffID), row.CreatedAt.UTC(),
		)
		if err != nil {
			return mapDBErr(err)
		}
		return p.insertHistory(ctx, []CaseHistory{history})
	})
}

func (p *PostgresStore) ListEvidence(ctx context.Context, caseID ID, limit int) ([]CaseEvidence, error) {
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
		limit = DefaultEvidenceLimit
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, case_id, evidence_type, title, description, reference_value, actor_staff_id, created_at
		FROM moderation.case_evidence
		WHERE case_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT $2`, caseID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]CaseEvidence, 0)
	for rows.Next() {
		row, err := scanEvidence(rows)
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

func scanEvidence(row interface {
	Scan(dest ...any) error
}) (CaseEvidence, error) {
	var e CaseEvidence
	var kind string
	if err := row.Scan(
		&e.ID, &e.CaseID, &kind, &e.Title, &e.Description, &e.ReferenceValue, &e.ActorStaffID, &e.CreatedAt,
	); err != nil {
		return CaseEvidence{}, mapDBErr(err)
	}
	e.EvidenceType = EvidenceType(kind)
	return e, nil
}

func (p *PostgresStore) assertReportsAttachable(ctx context.Context, row Case, reportIDs []ID) error {
	seen := make(map[ID]struct{}, len(reportIDs))
	for _, reportID := range reportIDs {
		if reportID.IsZero() {
			return errZeroID
		}
		if _, dup := seen[reportID]; dup {
			return errConflict
		}
		seen[reportID] = struct{}{}
		report, err := p.GetByID(ctx, reportID)
		if err != nil {
			return err
		}
		if report.TargetType != row.SubjectType || report.TargetID != row.SubjectID {
			return errInvalidAttachment
		}
		var existing ID
		scanErr := p.db.QueryRow(ctx, `
			SELECT case_id FROM moderation.case_reports WHERE report_id = $1`, reportID).Scan(&existing)
		if scanErr == nil {
			return errConflict
		}
		if mapDBErr(scanErr) != errNotFound {
			return mapDBErr(scanErr)
		}
	}
	return nil
}

func (p *PostgresStore) insertCaseReports(ctx context.Context, caseID ID, reportIDs []ID, attachedAt time.Time) error {
	for _, reportID := range reportIDs {
		if _, err := p.db.Exec(ctx, `
			INSERT INTO moderation.case_reports (case_id, report_id, attached_at)
			VALUES ($1, $2, $3)`, caseID, reportID, attachedAt.UTC()); err != nil {
			return mapDBErr(err)
		}
	}
	return nil
}

func (p *PostgresStore) insertHistory(ctx context.Context, history []CaseHistory) error {
	for _, h := range history {
		if _, err := p.db.Exec(ctx, `
			INSERT INTO moderation.case_history (
				id, case_id, kind, actor_staff_id, report_id,
				from_status, to_status, from_priority, to_priority,
				from_assigned_staff_id, to_assigned_staff_id, note, evidence_id, action_id, appeal_id, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
			h.ID, h.CaseID, string(h.Kind), uuidOrNil(h.ActorStaffID), uuidOrNil(h.ReportID),
			statusOrNil(h.FromStatus), statusOrNil(h.ToStatus), priorityOrNil(h.FromPriority), priorityOrNil(h.ToPriority),
			uuidOrNil(h.FromAssignedStaffID), uuidOrNil(h.ToAssignedStaffID), h.Note, uuidOrNil(h.EvidenceID), uuidOrNil(h.ActionID), uuidOrNil(h.AppealID), h.CreatedAt.UTC(),
		); err != nil {
			return mapDBErr(err)
		}
	}
	return nil
}

func statusOrNil(s *CaseStatus) any {
	if s == nil {
		return nil
	}
	return string(*s)
}

func priorityOrNil(p *CasePriority) any {
	if p == nil {
		return nil
	}
	return string(*p)
}

func scanCase(row interface {
	Scan(dest ...any) error
}) (Case, error) {
	var c Case
	var status, priority, subjectType string
	var assigned *ID
	if err := row.Scan(
		&c.ID, &status, &priority, &subjectType, &c.SubjectID, &c.Title, &assigned, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return Case{}, mapDBErr(err)
	}
	c.Status = CaseStatus(status)
	c.Priority = CasePriority(priority)
	c.SubjectType = TargetType(subjectType)
	c.AssignedStaffID = assigned
	return c, nil
}

func scanHistory(row interface {
	Scan(dest ...any) error
}) (CaseHistory, error) {
	var h CaseHistory
	var kind string
	var fromStatus, toStatus, fromPriority, toPriority *string
	if err := row.Scan(
		&h.ID, &h.CaseID, &kind, &h.ActorStaffID, &h.ReportID,
		&fromStatus, &toStatus, &fromPriority, &toPriority,
		&h.FromAssignedStaffID, &h.ToAssignedStaffID, &h.Note, &h.EvidenceID, &h.ActionID, &h.AppealID, &h.CreatedAt,
	); err != nil {
		return CaseHistory{}, mapDBErr(err)
	}
	h.Kind = CaseHistoryKind(kind)
	if fromStatus != nil {
		v := CaseStatus(*fromStatus)
		h.FromStatus = &v
	}
	if toStatus != nil {
		v := CaseStatus(*toStatus)
		h.ToStatus = &v
	}
	if fromPriority != nil {
		v := CasePriority(*fromPriority)
		h.FromPriority = &v
	}
	if toPriority != nil {
		v := CasePriority(*toPriority)
		h.ToPriority = &v
	}
	return h, nil
}
