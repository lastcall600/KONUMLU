package moderation

import (
	"context"
	"errors"
	"strings"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
	"backend/internal/platform/outbox"
)

type Service struct {
	store          persistence
	listings       listingcontracts.Ownership
	listingEnforce listingcontracts.ModerationEnforcement
	profiles       identitycontracts.PublicProfileResolver
	profileEnforce identitycontracts.PublicProfileModeration
	outbox         warningEnqueuer
	policy         Policy
	now            func() time.Time
}

type warningEnqueuer interface {
	Enqueue(ctx context.Context, exec outbox.Execer, in outbox.NewEvent) (outbox.Event, error)
}

func NewService(store persistence, listings listingcontracts.Ownership, profiles identitycontracts.PublicProfileResolver, policy Policy, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if listings == nil {
		return nil, errListingsReq
	}
	if profiles == nil {
		return nil, errProfilesReq
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	svc := &Service{store: store, listings: listings, profiles: profiles, policy: policy, now: now}
	if e, ok := listings.(listingcontracts.ModerationEnforcement); ok {
		svc.listingEnforce = e
	}
	if e, ok := profiles.(identitycontracts.PublicProfileModeration); ok {
		svc.profileEnforce = e
	}
	return svc, nil
}

// SetListingEnforcement wires Listings hide/remove/restore. Nil disables listing restrict/remove execution and appeal restoration.
func (s *Service) SetListingEnforcement(e listingcontracts.ModerationEnforcement) {
	if s == nil {
		return
	}
	s.listingEnforce = e
}

// SetProfileEnforcement wires Identity public-profile hide/remove/restore. Nil disables public_profile restrict/remove execution and appeal restoration.
func (s *Service) SetProfileEnforcement(e identitycontracts.PublicProfileModeration) {
	if s == nil {
		return
	}
	s.profileEnforce = e
}

// SetOutbox wires the platform outbox for warning notification intents. Nil disables warning execution.
func (s *Service) SetOutbox(enq warningEnqueuer) {
	if s == nil {
		return
	}
	s.outbox = enq
}

func (s *Service) Create(ctx context.Context, reporterUserID ID, in CreateInput) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, errStoreRequired
	}
	if reporterUserID.IsZero() || in.TargetID.IsZero() {
		return Report{}, errZeroID
	}
	targetType, err := ParseTargetType(string(in.TargetType))
	if err != nil {
		return Report{}, err
	}
	reason, err := ParseReasonCode(string(in.ReasonCode))
	if err != nil {
		return Report{}, err
	}
	description, err := NormalizeDescription(in.Description)
	if err != nil {
		return Report{}, err
	}
	if err := s.assertReportableTarget(ctx, reporterUserID, targetType, in.TargetID); err != nil {
		return Report{}, err
	}
	since := s.now().UTC().Add(-s.policy.DuplicateWindow)
	_, err = s.store.FindRecentIdentical(ctx, reporterUserID, targetType, in.TargetID, reason, since)
	if err == nil {
		return Report{}, errConflict
	}
	if !errors.Is(err, errNotFound) {
		return Report{}, mapStoreErr(err)
	}
	id, err := NewID()
	if err != nil {
		return Report{}, errUnavailable
	}
	now := s.now().UTC()
	row := Report{
		ID:             id,
		ReporterUserID: reporterUserID,
		TargetType:     targetType,
		TargetID:       in.TargetID,
		ReasonCode:     reason,
		Description:    description,
		Status:         StatusSubmitted,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := row.Validate(); err != nil {
		return Report{}, err
	}
	if err := s.store.Insert(ctx, row); err != nil {
		return Report{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) ListMine(ctx context.Context, reporterUserID ID) ([]Report, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if reporterUserID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListForReporter(ctx, reporterUserID, MaxMineReports)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) GetReport(ctx context.Context, id ID) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, errStoreRequired
	}
	if id.IsZero() {
		return Report{}, errZeroID
	}
	row, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Report{}, mapStoreErr(err)
	}
	return row, nil
}

func (s *Service) ListQueue(ctx context.Context, q QueueQuery) (QueuePage, error) {
	if s == nil || s.store == nil {
		return QueuePage{}, errStoreRequired
	}
	q, err := normalizeQueueQuery(q)
	if err != nil {
		return QueuePage{}, err
	}
	var cursor *queueCursor
	if strings.TrimSpace(q.Cursor) != "" {
		decoded, err := decodeQueueCursor(q.Cursor, q.Order)
		if err != nil {
			return QueuePage{}, err
		}
		cursor = &decoded
	}
	rows, err := s.store.ListQueue(ctx, q, cursor, q.Limit)
	if err != nil {
		return QueuePage{}, mapStoreErr(err)
	}
	page := QueuePage{Reports: rows}
	if len(rows) == q.Limit {
		next, err := encodeQueueCursor(q.Order, rows[len(rows)-1])
		if err != nil {
			return QueuePage{}, err
		}
		page.NextCursor = next
	}
	return page, nil
}

func (s *Service) Transition(ctx context.Context, id ID, in TransitionInput) (Report, error) {
	if s == nil || s.store == nil {
		return Report{}, errStoreRequired
	}
	if id.IsZero() {
		return Report{}, errZeroID
	}
	to, err := ParseStatus(string(in.To))
	if err != nil {
		return Report{}, err
	}
	note, err := NormalizeStaffNote(in.StaffNote)
	if err != nil {
		return Report{}, err
	}
	var actor *ID
	if in.ActorID != nil && !in.ActorID.IsZero() {
		idCopy := *in.ActorID
		actor = &idCopy
	}
	row, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Report{}, mapStoreErr(err)
	}
	if !CanTransition(row.Status, to) {
		return Report{}, errInvalidTransition
	}
	now := s.now().UTC()
	if err := s.store.UpdateStatus(ctx, id, row.Status, to, now, note, actor); err != nil {
		return Report{}, mapStoreErr(err)
	}
	updated, err := s.store.GetByID(ctx, id)
	if err != nil {
		return Report{}, mapStoreErr(err)
	}
	return updated, nil
}

func (s *Service) assertReportableTarget(ctx context.Context, reporterUserID ID, targetType TargetType, targetID ID) error {
	switch targetType {
	case TargetListing:
		return s.assertReportableListing(ctx, reporterUserID, targetID)
	case TargetPublicProfile:
		return s.assertReportableProfile(ctx, reporterUserID, targetID)
	default:
		return errInvalidTarget
	}
}

func (s *Service) assertReportableListing(ctx context.Context, reporterUserID, listingID ID) error {
	if s.listings == nil {
		return errListingsReq
	}
	ref, err := s.listings.ResolveListingOwner(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
			return errNotFound
		}
		if errors.Is(err, listingcontracts.ErrZeroID) {
			return errZeroID
		}
		return errUnavailable
	}
	if !ref.PubliclyVisible() {
		return errNotFound
	}
	if ID(ref.OwnerUserID) == reporterUserID {
		return errSelfReport
	}
	return nil
}

func (s *Service) assertReportableProfile(ctx context.Context, reporterUserID, publicProfileID ID) error {
	if s.profiles == nil {
		return errProfilesReq
	}
	if _, err := s.profiles.ResolveByPublicID(ctx, identitycontracts.ID(publicProfileID)); err != nil {
		return mapProfileErr(err)
	}
	ownerID, err := s.profiles.ResolveUserIDByPublicID(ctx, identitycontracts.ID(publicProfileID))
	if err != nil {
		return mapProfileErr(err)
	}
	if ID(ownerID) == reporterUserID {
		return errSelfReport
	}
	return nil
}

func mapProfileErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, identitycontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, identitycontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, identitycontracts.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, identitycontracts.ErrInvalidModerationState) || errors.Is(err, identitycontracts.ErrModerationNotEnforced) {
		return errInvalidAction
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errZeroID) || errors.Is(err, errInvalidTarget) || errors.Is(err, errInvalidReason) ||
		errors.Is(err, errInvalidStatus) || errors.Is(err, errInvalidBody) || errors.Is(err, errInvalidReport) ||
		errors.Is(err, errSelfReport) || errors.Is(err, errNotFound) || errors.Is(err, errConflict) ||
		errors.Is(err, errStoreRequired) || errors.Is(err, errUnavailable) || errors.Is(err, errListingsReq) ||
		errors.Is(err, errProfilesReq) || errors.Is(err, errInvalidQuery) || errors.Is(err, errInvalidTransition) ||
		errors.Is(err, errInvalidPriority) || errors.Is(err, errInvalidCaseStatus) || errors.Is(err, errInvalidCase) ||
		errors.Is(err, errInvalidAttachment) || errors.Is(err, errInvalidHistory) || errors.Is(err, errInvalidEvidence) ||
		errors.Is(err, errInvalidAction) || errors.Is(err, errInvalidActionStatus) ||
		errors.Is(err, errInvalidAppeal) || errors.Is(err, errInvalidAppealStatus) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
