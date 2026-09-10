package moderation

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"
)

var (
	_ reportStore = (*MemoryStore)(nil)
	_ caseStore   = (*MemoryStore)(nil)
	_ persistence = (*MemoryStore)(nil)
)

type MemoryStore struct {
	mu          sync.Mutex
	byID        map[ID]Report
	cases       map[ID]Case
	caseReports map[ID][]ID
	reportCase  map[ID]ID
	history     map[ID][]CaseHistory
	evidence    map[ID][]CaseEvidence
	actions     map[ID]CaseAction
	appeals     map[ID]Appeal
	fail        error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:        make(map[ID]Report),
		cases:       make(map[ID]Case),
		caseReports: make(map[ID][]ID),
		reportCase:  make(map[ID]ID),
		history:     make(map[ID][]CaseHistory),
		evidence:    make(map[ID][]CaseEvidence),
		actions:     make(map[ID]CaseAction),
		appeals:     make(map[ID]Appeal),
	}
}

func (m *MemoryStore) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return errUnavailable
	}
	return fn(ctx)
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) GetByID(ctx context.Context, id ID) (Report, error) {
	if m == nil {
		return Report{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if id.IsZero() {
		return Report{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Report{}, m.fail
	}
	row, ok := m.byID[id]
	if !ok {
		return Report{}, errNotFound
	}
	return cloneReport(row), nil
}

func (m *MemoryStore) Insert(ctx context.Context, row Report) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := row.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.byID[row.ID]; ok {
		return errConflict
	}
	if openIdentical(m.byID, row) {
		return errConflict
	}
	m.byID[row.ID] = cloneReport(row)
	return nil
}

func (m *MemoryStore) FindRecentIdentical(ctx context.Context, reporterUserID ID, targetType TargetType, targetID ID, reason ReasonCode, since time.Time) (Report, error) {
	if m == nil {
		return Report{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Report{}, m.fail
	}
	var found *Report
	for _, row := range m.byID {
		if row.ReporterUserID != reporterUserID || row.TargetType != targetType || row.TargetID != targetID || row.ReasonCode != reason {
			continue
		}
		if row.CreatedAt.Before(since) {
			continue
		}
		clone := cloneReport(row)
		if found == nil || clone.CreatedAt.After(found.CreatedAt) || (clone.CreatedAt.Equal(found.CreatedAt) && clone.ID.String() > found.ID.String()) {
			found = &clone
		}
	}
	if found == nil {
		return Report{}, errNotFound
	}
	return *found, nil
}

func (m *MemoryStore) ListForReporter(ctx context.Context, reporterUserID ID, limit int) ([]Report, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if limit <= 0 {
		limit = MaxMineReports
	}
	out := make([]Report, 0)
	for _, row := range m.byID {
		if row.ReporterUserID == reporterUserID {
			out = append(out, cloneReport(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) ListQueue(ctx context.Context, q QueueQuery, cursor *queueCursor, limit int) ([]Report, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if limit <= 0 {
		limit = DefaultQueueLimit
	}
	out := make([]Report, 0)
	for _, row := range m.byID {
		if !matchesQueueFilters(row, q) {
			continue
		}
		if !afterQueueCursor(row, cursor, q.Order) {
			continue
		}
		out = append(out, cloneReport(row))
	}
	sort.Slice(out, func(i, j int) bool {
		cmp := compareQueue(out[i], out[j])
		if q.Order == QueueOldest {
			return cmp < 0
		}
		return cmp > 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) UpdateStatus(ctx context.Context, id ID, from, to Status, updatedAt time.Time, note *string, actor *ID) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if id.IsZero() {
		return errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	row, ok := m.byID[id]
	if !ok {
		return errNotFound
	}
	if row.Status != from {
		return errConflict
	}
	row.Status = to
	row.UpdatedAt = updatedAt.UTC()
	if note != nil {
		v := *note
		row.StaffNote = &v
	}
	if actor != nil {
		idCopy := *actor
		row.StatusChangedBy = &idCopy
	}
	m.byID[id] = row
	return nil
}

func openIdentical(rows map[ID]Report, next Report) bool {
	for _, row := range rows {
		if row.ReporterUserID != next.ReporterUserID || row.TargetType != next.TargetType || row.TargetID != next.TargetID || row.ReasonCode != next.ReasonCode {
			continue
		}
		if row.Status == StatusSubmitted || row.Status == StatusTriaged {
			return true
		}
	}
	return false
}

func cloneReport(row Report) Report {
	out := row
	if row.Description != nil {
		v := *row.Description
		out.Description = &v
	}
	if row.StaffNote != nil {
		v := *row.StaffNote
		out.StaffNote = &v
	}
	if row.StatusChangedBy != nil {
		idCopy := *row.StatusChangedBy
		out.StatusChangedBy = &idCopy
	}
	return out
}

func (m *MemoryStore) InsertCase(ctx context.Context, row Case, reportIDs []ID, history []CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.cases[row.ID]; ok {
		return errConflict
	}
	if err := m.assertReportsAttachableLocked(row, reportIDs); err != nil {
		return err
	}
	m.cases[row.ID] = cloneCase(row)
	ids := append([]ID(nil), reportIDs...)
	m.caseReports[row.ID] = ids
	for _, reportID := range ids {
		m.reportCase[reportID] = row.ID
	}
	cloned := make([]CaseHistory, 0, len(history))
	for _, h := range history {
		cloned = append(cloned, cloneHistory(h))
	}
	m.history[row.ID] = cloned
	return nil
}

func (m *MemoryStore) GetCase(ctx context.Context, id ID) (Case, error) {
	if m == nil {
		return Case{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Case{}, err
	}
	if id.IsZero() {
		return Case{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Case{}, m.fail
	}
	row, ok := m.cases[id]
	if !ok {
		return Case{}, errNotFound
	}
	return cloneCase(row), nil
}

func (m *MemoryStore) ListCases(ctx context.Context, q CaseQuery, cursor *caseCursor, limit int) ([]Case, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if limit <= 0 {
		limit = DefaultCaseLimit
	}
	out := make([]Case, 0)
	for _, row := range m.cases {
		if !matchesCaseFilters(row, q) {
			continue
		}
		if !afterCaseCursor(row, cursor) {
			continue
		}
		out = append(out, cloneCase(row))
	}
	sort.Slice(out, func(i, j int) bool {
		return compareCase(out[i], out[j]) > 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) ListCaseReportIDs(ctx context.Context, caseID ID) ([]ID, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if _, ok := m.cases[caseID]; !ok {
		return nil, errNotFound
	}
	ids := m.caseReports[caseID]
	out := append([]ID(nil), ids...)
	return out, nil
}

func (m *MemoryStore) ListCaseHistory(ctx context.Context, caseID ID) ([]CaseHistory, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if _, ok := m.cases[caseID]; !ok {
		return nil, errNotFound
	}
	rows := m.history[caseID]
	out := make([]CaseHistory, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneHistory(row))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
	})
	return out, nil
}

func (m *MemoryStore) UpdateCase(ctx context.Context, from, to Case, history CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	row, ok := m.cases[to.ID]
	if !ok {
		return errNotFound
	}
	if row.Status != from.Status || row.Priority != from.Priority || !sameOptionalID(row.AssignedStaffID, from.AssignedStaffID) {
		return errConflict
	}
	m.cases[to.ID] = cloneCase(to)
	m.history[to.ID] = append(m.history[to.ID], cloneHistory(history))
	return nil
}

func (m *MemoryStore) AttachCaseReports(ctx context.Context, caseID ID, reportIDs []ID, updatedAt time.Time, history []CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	row, ok := m.cases[caseID]
	if !ok {
		return errNotFound
	}
	if !row.Status.IsActive() {
		return errInvalidAttachment
	}
	if err := m.assertReportsAttachableLocked(row, reportIDs); err != nil {
		return err
	}
	row.UpdatedAt = updatedAt.UTC()
	m.cases[caseID] = row
	m.caseReports[caseID] = append(m.caseReports[caseID], reportIDs...)
	for _, reportID := range reportIDs {
		m.reportCase[reportID] = caseID
	}
	for _, h := range history {
		m.history[caseID] = append(m.history[caseID], cloneHistory(h))
	}
	return nil
}

func (m *MemoryStore) InsertEvidence(ctx context.Context, row CaseEvidence, history CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.cases[row.CaseID]; !ok {
		return errNotFound
	}
	for _, existing := range m.evidence[row.CaseID] {
		if existing.ID == row.ID {
			return errConflict
		}
	}
	m.evidence[row.CaseID] = append(m.evidence[row.CaseID], cloneEvidence(row))
	m.history[row.CaseID] = append(m.history[row.CaseID], cloneHistory(history))
	return nil
}

func (m *MemoryStore) ListEvidence(ctx context.Context, caseID ID, limit int) ([]CaseEvidence, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if _, ok := m.cases[caseID]; !ok {
		return nil, errNotFound
	}
	if limit <= 0 {
		limit = DefaultEvidenceLimit
	}
	rows := m.evidence[caseID]
	out := make([]CaseEvidence, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneEvidence(row))
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) InsertAction(ctx context.Context, row CaseAction, history CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.cases[row.CaseID]; !ok {
		return errNotFound
	}
	if _, ok := m.actions[row.ID]; ok {
		return errConflict
	}
	m.actions[row.ID] = cloneAction(row)
	m.history[row.CaseID] = append(m.history[row.CaseID], cloneHistory(history))
	return nil
}

func (m *MemoryStore) GetAction(ctx context.Context, id ID) (CaseAction, error) {
	if m == nil {
		return CaseAction{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return CaseAction{}, err
	}
	if id.IsZero() {
		return CaseAction{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return CaseAction{}, m.fail
	}
	row, ok := m.actions[id]
	if !ok {
		return CaseAction{}, errNotFound
	}
	return cloneAction(row), nil
}

func (m *MemoryStore) ListActions(ctx context.Context, caseID ID, limit int) ([]CaseAction, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if _, ok := m.cases[caseID]; !ok {
		return nil, errNotFound
	}
	if limit <= 0 {
		limit = DefaultActionLimit
	}
	out := make([]CaseAction, 0)
	for _, row := range m.actions {
		if row.CaseID == caseID {
			out = append(out, cloneAction(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) UpdateAction(ctx context.Context, from, to CaseAction, history CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	row, ok := m.actions[to.ID]
	if !ok {
		return errNotFound
	}
	if row.Status != from.Status {
		return errConflict
	}
	if row.TargetType != from.TargetType || row.TargetID != from.TargetID ||
		row.ActionType != from.ActionType || row.ReasonCode != from.ReasonCode {
		return errConflict
	}
	m.actions[to.ID] = cloneAction(to)
	m.history[to.CaseID] = append(m.history[to.CaseID], cloneHistory(history))
	return nil
}

func (m *MemoryStore) InsertAppeal(ctx context.Context, row Appeal, history CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.cases[row.CaseID]; !ok {
		return errNotFound
	}
	if _, ok := m.actions[row.ActionID]; !ok {
		return errNotFound
	}
	if _, ok := m.appeals[row.ID]; ok {
		return errConflict
	}
	if activeAppealLocked(m.appeals, row.ActionID, row.AppellantUserID) {
		return errConflict
	}
	m.appeals[row.ID] = cloneAppeal(row)
	m.history[row.CaseID] = append(m.history[row.CaseID], cloneHistory(history))
	return nil
}

func (m *MemoryStore) GetAppeal(ctx context.Context, id ID) (Appeal, error) {
	if m == nil {
		return Appeal{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Appeal{}, err
	}
	if id.IsZero() {
		return Appeal{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Appeal{}, m.fail
	}
	row, ok := m.appeals[id]
	if !ok {
		return Appeal{}, errNotFound
	}
	return cloneAppeal(row), nil
}

func (m *MemoryStore) FindActiveAppeal(ctx context.Context, actionID, appellantUserID ID) (Appeal, error) {
	if m == nil {
		return Appeal{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Appeal{}, err
	}
	if actionID.IsZero() || appellantUserID.IsZero() {
		return Appeal{}, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Appeal{}, m.fail
	}
	for _, row := range m.appeals {
		if row.ActionID == actionID && row.AppellantUserID == appellantUserID && row.Status.IsActive() {
			return cloneAppeal(row), nil
		}
	}
	return Appeal{}, errNotFound
}

func (m *MemoryStore) ListAppeals(ctx context.Context, caseID ID, limit int) ([]Appeal, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if _, ok := m.cases[caseID]; !ok {
		return nil, errNotFound
	}
	if limit <= 0 {
		limit = DefaultAppealLimit
	}
	out := make([]Appeal, 0)
	for _, row := range m.appeals {
		if row.CaseID == caseID {
			out = append(out, cloneAppeal(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) < 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) ListAppealsForAppellant(ctx context.Context, appellantUserID ID, limit int) ([]Appeal, error) {
	if m == nil {
		return nil, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	if limit <= 0 {
		limit = MaxMineAppeals
	}
	out := make([]Appeal, 0)
	for _, row := range m.appeals {
		if row.AppellantUserID == appellantUserID {
			out = append(out, cloneAppeal(row))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return bytes.Compare(out[i].ID[:], out[j].ID[:]) > 0
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) UpdateAppeal(ctx context.Context, from, to Appeal, history []CaseHistory) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
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
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	row, ok := m.appeals[to.ID]
	if !ok {
		return errNotFound
	}
	if row.Status != from.Status {
		return errConflict
	}
	m.appeals[to.ID] = cloneAppeal(to)
	for _, h := range history {
		m.history[to.CaseID] = append(m.history[to.CaseID], cloneHistory(h))
	}
	return nil
}

func activeAppealLocked(appeals map[ID]Appeal, actionID, appellantUserID ID) bool {
	for _, row := range appeals {
		if row.ActionID == actionID && row.AppellantUserID == appellantUserID && row.Status.IsActive() {
			return true
		}
	}
	return false
}

func (m *MemoryStore) assertReportsAttachableLocked(row Case, reportIDs []ID) error {
	seen := make(map[ID]struct{}, len(reportIDs))
	for _, reportID := range reportIDs {
		if reportID.IsZero() {
			return errZeroID
		}
		if _, dup := seen[reportID]; dup {
			return errConflict
		}
		seen[reportID] = struct{}{}
		report, ok := m.byID[reportID]
		if !ok {
			return errNotFound
		}
		if report.TargetType != row.SubjectType || report.TargetID != row.SubjectID {
			return errInvalidAttachment
		}
		if existing, ok := m.reportCase[reportID]; ok {
			if existing == row.ID {
				return errConflict
			}
			other, found := m.cases[existing]
			if found && other.Status.IsActive() {
				return errConflict
			}
			return errConflict
		}
	}
	return nil
}
