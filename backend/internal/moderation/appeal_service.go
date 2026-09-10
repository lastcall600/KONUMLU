package moderation

import (
	"context"
	"errors"
	"time"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
)

func (s *Service) SubmitAppeal(ctx context.Context, appellantUserID ID, in CreateAppealInput) (Appeal, error) {
	if s == nil || s.store == nil {
		return Appeal{}, errStoreRequired
	}
	if appellantUserID.IsZero() || in.ActionID.IsZero() {
		return Appeal{}, errZeroID
	}
	statement, err := NormalizeAppealStatement(in.Statement)
	if err != nil {
		return Appeal{}, err
	}
	action, err := s.store.GetAction(ctx, in.ActionID)
	if err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	if err := s.assertAppealSubject(ctx, appellantUserID, action); err != nil {
		return Appeal{}, err
	}
	if action.Status != ActionStatusExecuted {
		return Appeal{}, errNotFound
	}
	if s.now().UTC().Sub(action.UpdatedAt.UTC()) > s.policy.AppealWindow {
		return Appeal{}, errInvalidTransition
	}
	_, err = s.store.FindActiveAppeal(ctx, action.ID, appellantUserID)
	if err == nil {
		return Appeal{}, errConflict
	}
	if !errors.Is(err, errNotFound) {
		return Appeal{}, mapStoreErr(err)
	}
	id, err := NewID()
	if err != nil {
		return Appeal{}, errUnavailable
	}
	now := s.now().UTC()
	row := Appeal{
		ID:              id,
		ActionID:        action.ID,
		CaseID:          action.CaseID,
		AppellantUserID: appellantUserID,
		Statement:       statement,
		Status:          AppealStatusSubmitted,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := row.Validate(); err != nil {
		return Appeal{}, err
	}
	hist, err := s.newHistory(action.CaseID, HistoryAppealSubmitted, nil, now)
	if err != nil {
		return Appeal{}, err
	}
	hist.AppealID = &id
	if err := hist.Validate(); err != nil {
		return Appeal{}, err
	}
	if err := s.store.InsertAppeal(ctx, row, hist); err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	return cloneAppeal(row), nil
}

func (s *Service) ListMineAppeals(ctx context.Context, appellantUserID ID) ([]Appeal, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if appellantUserID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListAppealsForAppellant(ctx, appellantUserID, MaxMineAppeals)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) GetAppealForUser(ctx context.Context, appellantUserID, appealID ID) (Appeal, error) {
	if s == nil || s.store == nil {
		return Appeal{}, errStoreRequired
	}
	if appellantUserID.IsZero() || appealID.IsZero() {
		return Appeal{}, errZeroID
	}
	row, err := s.store.GetAppeal(ctx, appealID)
	if err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	if row.AppellantUserID != appellantUserID {
		return Appeal{}, errNotFound
	}
	return row, nil
}

func (s *Service) WithdrawAppeal(ctx context.Context, appellantUserID, appealID ID) (Appeal, error) {
	if s == nil || s.store == nil {
		return Appeal{}, errStoreRequired
	}
	if appellantUserID.IsZero() || appealID.IsZero() {
		return Appeal{}, errZeroID
	}
	row, err := s.store.GetAppeal(ctx, appealID)
	if err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	if row.AppellantUserID != appellantUserID {
		return Appeal{}, errNotFound
	}
	if !CanTransitionAppeal(row.Status, AppealStatusWithdrawn) {
		return Appeal{}, errInvalidTransition
	}
	now := s.now().UTC()
	updated := cloneAppeal(row)
	updated.Status = AppealStatusWithdrawn
	updated.UpdatedAt = now
	updated.DecidedAt = &now
	updated.DecidedByStaffID = nil
	return s.persistAppealTransition(ctx, row, updated, nil, nil)
}

func (s *Service) GetAppeal(ctx context.Context, caseID, appealID ID) (Appeal, error) {
	if s == nil || s.store == nil {
		return Appeal{}, errStoreRequired
	}
	if caseID.IsZero() || appealID.IsZero() {
		return Appeal{}, errZeroID
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	row, err := s.store.GetAppeal(ctx, appealID)
	if err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	if row.CaseID != caseID {
		return Appeal{}, errNotFound
	}
	return row, nil
}

func (s *Service) ListAppeals(ctx context.Context, caseID ID, limit int) ([]Appeal, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if limit < 0 || limit > MaxAppealLimit {
		return nil, errInvalidQuery
	}
	if limit == 0 {
		limit = DefaultAppealLimit
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return nil, mapStoreErr(err)
	}
	rows, err := s.store.ListAppeals(ctx, caseID, limit)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) TransitionAppeal(ctx context.Context, caseID, appealID ID, in AppealTransitionInput) (Appeal, error) {
	if s == nil || s.store == nil {
		return Appeal{}, errStoreRequired
	}
	if caseID.IsZero() || appealID.IsZero() {
		return Appeal{}, errZeroID
	}
	to, err := ParseAppealStatus(string(in.To))
	if err != nil {
		return Appeal{}, err
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	row, err := s.store.GetAppeal(ctx, appealID)
	if err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	if row.CaseID != caseID {
		return Appeal{}, errNotFound
	}
	if !CanStaffTransitionAppeal(row.Status, to) {
		return Appeal{}, errInvalidTransition
	}
	now := s.now().UTC()
	updated := cloneAppeal(row)
	updated.Status = to
	updated.UpdatedAt = now
	if to == AppealStatusAccepted || to == AppealStatusRejected {
		updated.DecidedAt = &now
		updated.DecidedByStaffID = optionalID(in.ActorID)
	}
	var extra []CaseHistory
	if to == AppealStatusAccepted {
		action, err := s.store.GetAction(ctx, row.ActionID)
		if err != nil {
			return Appeal{}, mapStoreErr(err)
		}
		if listingAppealNeedsRestore(action) {
			if err := s.clearListingModeration(ctx, action); err != nil {
				return Appeal{}, err
			}
			restHist, err := s.restorationHistory(row, action, optionalID(in.ActorID), now)
			if err != nil {
				return Appeal{}, err
			}
			extra = append(extra, restHist)
			updated.UpdatedAt = now.Add(time.Nanosecond)
			if updated.DecidedAt != nil {
				decided := updated.UpdatedAt
				updated.DecidedAt = &decided
			}
		} else if profileAppealNeedsRestore(action) {
			if err := s.clearProfileModeration(ctx, action); err != nil {
				return Appeal{}, err
			}
			restHist, err := s.restorationHistory(row, action, optionalID(in.ActorID), now)
			if err != nil {
				return Appeal{}, err
			}
			extra = append(extra, restHist)
			updated.UpdatedAt = now.Add(time.Nanosecond)
			if updated.DecidedAt != nil {
				decided := updated.UpdatedAt
				updated.DecidedAt = &decided
			}
		}
	}
	return s.persistAppealTransition(ctx, row, updated, optionalID(in.ActorID), extra)
}

func (s *Service) persistAppealTransition(ctx context.Context, from, to Appeal, actor *ID, extra []CaseHistory) (Appeal, error) {
	if err := to.Validate(); err != nil {
		return Appeal{}, err
	}
	kind, err := appealHistoryKind(to.Status)
	if err != nil {
		return Appeal{}, err
	}
	hist, err := s.newHistory(to.CaseID, kind, actor, to.UpdatedAt)
	if err != nil {
		return Appeal{}, err
	}
	hist.AppealID = &to.ID
	if err := hist.Validate(); err != nil {
		return Appeal{}, err
	}
	history := append(append([]CaseHistory{}, extra...), hist)
	if err := s.store.UpdateAppeal(ctx, from, to, history); err != nil {
		return Appeal{}, mapStoreErr(err)
	}
	return cloneAppeal(to), nil
}

func listingAppealNeedsRestore(action CaseAction) bool {
	return action.TargetType == TargetListing &&
		(action.ActionType == ActionTypeRestrict || action.ActionType == ActionTypeRemove)
}

func profileAppealNeedsRestore(action CaseAction) bool {
	return action.TargetType == TargetPublicProfile &&
		(action.ActionType == ActionTypeRestrict || action.ActionType == ActionTypeRemove)
}

func (s *Service) restorationHistory(row Appeal, action CaseAction, actor *ID, now time.Time) (CaseHistory, error) {
	restHist, err := s.newHistory(row.CaseID, HistoryAppealRestorationApplied, actor, now)
	if err != nil {
		return CaseHistory{}, err
	}
	restHist.AppealID = &row.ID
	restHist.ActionID = &action.ID
	if err := restHist.Validate(); err != nil {
		return CaseHistory{}, err
	}
	return restHist, nil
}

func (s *Service) assertAppealSubject(ctx context.Context, userID ID, action CaseAction) error {
	switch action.TargetType {
	case TargetListing:
		return s.assertListingOwnedBy(ctx, userID, action.TargetID)
	case TargetPublicProfile:
		return s.assertProfileOwnedBy(ctx, userID, action.TargetID)
	default:
		return errInvalidTarget
	}
}

func (s *Service) assertListingOwnedBy(ctx context.Context, userID, listingID ID) error {
	if s.listings == nil {
		return errListingsReq
	}
	err := s.listings.AssertListingOwnedBy(ctx, listingcontracts.ID(listingID), listingcontracts.ID(userID))
	if err == nil {
		return nil
	}
	if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
		return errNotFound
	}
	if errors.Is(err, listingcontracts.ErrZeroID) {
		return errZeroID
	}
	return errUnavailable
}

func (s *Service) assertProfileOwnedBy(ctx context.Context, userID, publicProfileID ID) error {
	if s.profiles == nil {
		return errProfilesReq
	}
	ownerID, err := s.profiles.ResolveUserIDByPublicID(ctx, identitycontracts.ID(publicProfileID))
	if err != nil {
		if errors.Is(err, identitycontracts.ErrNotFound) {
			return errNotFound
		}
		return mapProfileErr(err)
	}
	if ID(ownerID) != userID {
		return errNotFound
	}
	return nil
}
