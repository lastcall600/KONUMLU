package moderation

import (
	"context"
	"time"
)

type persistence interface {
	reportStore
	caseStore
	WithTx(ctx context.Context, fn func(context.Context) error) error
}

type reportStore interface {
	Insert(ctx context.Context, row Report) error
	GetByID(ctx context.Context, id ID) (Report, error)
	FindRecentIdentical(ctx context.Context, reporterUserID ID, targetType TargetType, targetID ID, reason ReasonCode, since time.Time) (Report, error)
	ListForReporter(ctx context.Context, reporterUserID ID, limit int) ([]Report, error)
	ListQueue(ctx context.Context, q QueueQuery, cursor *queueCursor, limit int) ([]Report, error)
	UpdateStatus(ctx context.Context, id ID, from, to Status, updatedAt time.Time, note *string, actor *ID) error
}

type caseStore interface {
	InsertCase(ctx context.Context, row Case, reportIDs []ID, history []CaseHistory) error
	GetCase(ctx context.Context, id ID) (Case, error)
	ListCases(ctx context.Context, q CaseQuery, cursor *caseCursor, limit int) ([]Case, error)
	ListCaseReportIDs(ctx context.Context, caseID ID) ([]ID, error)
	ListCaseHistory(ctx context.Context, caseID ID) ([]CaseHistory, error)
	UpdateCase(ctx context.Context, from, to Case, history CaseHistory) error
	AttachCaseReports(ctx context.Context, caseID ID, reportIDs []ID, updatedAt time.Time, history []CaseHistory) error
	InsertEvidence(ctx context.Context, row CaseEvidence, history CaseHistory) error
	ListEvidence(ctx context.Context, caseID ID, limit int) ([]CaseEvidence, error)
	InsertAction(ctx context.Context, row CaseAction, history CaseHistory) error
	GetAction(ctx context.Context, id ID) (CaseAction, error)
	ListActions(ctx context.Context, caseID ID, limit int) ([]CaseAction, error)
	UpdateAction(ctx context.Context, from, to CaseAction, history CaseHistory) error
	InsertAppeal(ctx context.Context, row Appeal, history CaseHistory) error
	GetAppeal(ctx context.Context, id ID) (Appeal, error)
	FindActiveAppeal(ctx context.Context, actionID, appellantUserID ID) (Appeal, error)
	ListAppeals(ctx context.Context, caseID ID, limit int) ([]Appeal, error)
	ListAppealsForAppellant(ctx context.Context, appellantUserID ID, limit int) ([]Appeal, error)
	UpdateAppeal(ctx context.Context, from, to Appeal, history []CaseHistory) error
}
