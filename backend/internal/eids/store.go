package eids

import (
	"context"
	"time"
)

type store interface {
	Create(ctx context.Context, v Verification) error
	Get(ctx context.Context, id ID) (Verification, error)
	GetOpen(ctx context.Context, listingID ID, kind VerificationType) (Verification, error)
	GetLatest(ctx context.Context, listingID ID, kind VerificationType) (Verification, error)
	Update(ctx context.Context, v Verification, expectedUpdatedAt time.Time) error
	BindSubject(ctx context.Context, b SubjectBinding) error
	LookupSubject(ctx context.Context, subjectRef string) (SubjectBinding, error)
	LookupSubjectByVerification(ctx context.Context, verificationID ID) (SubjectBinding, error)
	GetReplay(ctx context.Context, decisionID string) (ReplayRecord, error)
	LatestReplayIssuedAt(ctx context.Context, subjectRef string) (time.Time, bool, error)
	InsertReplay(ctx context.Context, rec ReplayRecord) error
	ApplyReplayAndUpdate(ctx context.Context, rec ReplayRecord, next Verification, expectedUpdatedAt time.Time) error
}

// SubjectBinding is Germany-owned opaque subject_ref → listing verification.
// It is not TCKN, email, phone, or a reversible user id encoding.
type SubjectBinding struct {
	SubjectRef       string
	VerificationID   ID
	ListingID        ID
	VerificationType VerificationType
	CreatedAt        time.Time
}

// ReplayRecord is durable signed-decision idempotency. No provider payload.
type ReplayRecord struct {
	DecisionID       string
	ClaimsHash       [32]byte
	SubjectRef       string
	VerificationType VerificationType
	Status           string
	IssuedAt         time.Time
	ValidUntil       time.Time
	KeyID            string
	ReceivedAt       time.Time
	AppliedAt        time.Time
}
