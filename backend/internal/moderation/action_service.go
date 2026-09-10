package moderation

import (
	"context"
	"errors"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
)

func (s *Service) CreateAction(ctx context.Context, caseID ID, in CreateActionInput) (CaseAction, error) {
	if s == nil || s.store == nil {
		return CaseAction{}, errStoreRequired
	}
	if caseID.IsZero() || in.TargetID.IsZero() {
		return CaseAction{}, errZeroID
	}
	targetType, err := ParseTargetType(string(in.TargetType))
	if err != nil {
		return CaseAction{}, err
	}
	actionType, err := ParseActionType(string(in.ActionType))
	if err != nil {
		return CaseAction{}, err
	}
	reason, err := ParseActionReasonCode(string(in.ReasonCode))
	if err != nil {
		return CaseAction{}, err
	}
	rationale, err := NormalizeActionRationale(in.Rationale)
	if err != nil {
		return CaseAction{}, err
	}
	row, err := s.store.GetCase(ctx, caseID)
	if err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	if row.Status == CaseStatusClosed {
		return CaseAction{}, errInvalidTransition
	}
	if !actionMatchesCaseSubject(row, targetType, in.TargetID) {
		return CaseAction{}, errInvalidAction
	}
	id, err := NewID()
	if err != nil {
		return CaseAction{}, errUnavailable
	}
	now := s.now().UTC()
	action := CaseAction{
		ID:           id,
		CaseID:       caseID,
		TargetType:   targetType,
		TargetID:     in.TargetID,
		ActionType:   actionType,
		Status:       ActionStatusProposed,
		ReasonCode:   reason,
		Rationale:    rationale,
		ActorStaffID: optionalID(in.ActorID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := action.Validate(); err != nil {
		return CaseAction{}, err
	}
	hist, err := s.newHistory(caseID, HistoryActionProposed, optionalID(in.ActorID), now)
	if err != nil {
		return CaseAction{}, err
	}
	hist.ActionID = &id
	if err := hist.Validate(); err != nil {
		return CaseAction{}, err
	}
	if err := s.store.InsertAction(ctx, action, hist); err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	return cloneAction(action), nil
}

func (s *Service) GetAction(ctx context.Context, caseID, actionID ID) (CaseAction, error) {
	if s == nil || s.store == nil {
		return CaseAction{}, errStoreRequired
	}
	if caseID.IsZero() || actionID.IsZero() {
		return CaseAction{}, errZeroID
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	row, err := s.store.GetAction(ctx, actionID)
	if err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	if row.CaseID != caseID {
		return CaseAction{}, errNotFound
	}
	return row, nil
}

func (s *Service) ListActions(ctx context.Context, caseID ID, limit int) ([]CaseAction, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if caseID.IsZero() {
		return nil, errZeroID
	}
	if limit < 0 || limit > MaxActionLimit {
		return nil, errInvalidQuery
	}
	if limit == 0 {
		limit = DefaultActionLimit
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return nil, mapStoreErr(err)
	}
	rows, err := s.store.ListActions(ctx, caseID, limit)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return rows, nil
}

func (s *Service) TransitionAction(ctx context.Context, caseID, actionID ID, in ActionTransitionInput) (CaseAction, error) {
	if s == nil || s.store == nil {
		return CaseAction{}, errStoreRequired
	}
	if caseID.IsZero() || actionID.IsZero() {
		return CaseAction{}, errZeroID
	}
	to, err := ParseActionStatus(string(in.To))
	if err != nil {
		return CaseAction{}, err
	}
	if _, err := s.store.GetCase(ctx, caseID); err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	row, err := s.store.GetAction(ctx, actionID)
	if err != nil {
		return CaseAction{}, mapStoreErr(err)
	}
	if row.CaseID != caseID {
		return CaseAction{}, errNotFound
	}
	if !CanTransitionAction(row.Status, to) {
		return CaseAction{}, errInvalidTransition
	}
	if to == ActionStatusExecuted {
		if err := s.applyExecution(ctx, row); err != nil {
			return CaseAction{}, err
		}
	}
	now := s.now().UTC()
	updated := cloneAction(row)
	updated.Status = to
	updated.UpdatedAt = now
	updated.ActorStaffID = optionalID(in.ActorID)
	if err := updated.Validate(); err != nil {
		return CaseAction{}, err
	}
	kind, err := actionHistoryKind(to)
	if err != nil {
		return CaseAction{}, err
	}
	hist, err := s.newHistory(caseID, kind, optionalID(in.ActorID), now)
	if err != nil {
		return CaseAction{}, err
	}
	hist.ActionID = &actionID
	if err := hist.Validate(); err != nil {
		return CaseAction{}, err
	}
	persist := func(ctx context.Context) error {
		if to == ActionStatusExecuted && row.ActionType == ActionTypeWarning {
			if err := s.enqueueWarningIntent(ctx, updated); err != nil {
				return err
			}
		}
		return s.store.UpdateAction(ctx, row, updated, hist)
	}
	if to == ActionStatusExecuted && row.ActionType == ActionTypeWarning {
		if err := s.store.WithTx(ctx, persist); err != nil {
			return CaseAction{}, mapStoreErr(err)
		}
	} else {
		if err := persist(ctx); err != nil {
			return CaseAction{}, mapStoreErr(err)
		}
	}
	return cloneAction(updated), nil
}

func (s *Service) applyExecution(ctx context.Context, row CaseAction) error {
	switch row.TargetType {
	case TargetListing:
		return s.applyListingExecution(ctx, row)
	case TargetPublicProfile:
		return s.applyProfileExecution(ctx, row)
	default:
		return errInvalidTarget
	}
}

func (s *Service) applyListingExecution(ctx context.Context, row CaseAction) error {
	switch row.ActionType {
	case ActionTypeNoAction, ActionTypeWarning:
		return nil
	case ActionTypeSuspend:
		return errUnsupportedAction
	case ActionTypeRestrict:
		return s.applyListingModeration(ctx, row, listingcontracts.ModerationStateRestricted)
	case ActionTypeRemove:
		return s.applyListingModeration(ctx, row, listingcontracts.ModerationStateRemoved)
	default:
		return errInvalidAction
	}
}

func (s *Service) applyProfileExecution(ctx context.Context, row CaseAction) error {
	switch row.ActionType {
	case ActionTypeNoAction, ActionTypeWarning:
		return nil
	case ActionTypeSuspend:
		return errUnsupportedAction
	case ActionTypeRestrict:
		return s.applyProfileModeration(ctx, row, identitycontracts.ModerationStateRestricted)
	case ActionTypeRemove:
		return s.applyProfileModeration(ctx, row, identitycontracts.ModerationStateRemoved)
	default:
		return errInvalidAction
	}
}

func (s *Service) applyListingModeration(ctx context.Context, row CaseAction, state string) error {
	if s.listingEnforce == nil {
		return errUnavailable
	}
	err := s.listingEnforce.ApplyModerationState(ctx, listingcontracts.ApplyModerationInput{
		ListingID: listingcontracts.ID(row.TargetID),
		State:     state,
	})
	return mapListingEnforceErr(err)
}

func (s *Service) applyProfileModeration(ctx context.Context, row CaseAction, state string) error {
	if s.profileEnforce == nil {
		return errUnavailable
	}
	err := s.profileEnforce.ApplyModerationState(ctx, identitycontracts.ApplyPublicProfileModerationInput{
		PublicProfileID: identitycontracts.ID(row.TargetID),
		State:           state,
	})
	return mapProfileErr(err)
}

func (s *Service) clearListingModeration(ctx context.Context, row CaseAction) error {
	if s.listingEnforce == nil {
		return errUnavailable
	}
	err := s.listingEnforce.ClearModerationState(ctx, listingcontracts.ID(row.TargetID))
	return mapListingEnforceErr(err)
}

func (s *Service) clearProfileModeration(ctx context.Context, row CaseAction) error {
	if s.profileEnforce == nil {
		return errUnavailable
	}
	err := s.profileEnforce.ClearModerationState(ctx, identitycontracts.ID(row.TargetID))
	return mapProfileErr(err)
}

func mapListingEnforceErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, listingcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, listingcontracts.ErrNotFound) {
		return errNotFound
	}
	if errors.Is(err, listingcontracts.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, listingcontracts.ErrInvalidModerationState) || errors.Is(err, listingcontracts.ErrModerationNotEnforced) {
		return errInvalidAction
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
