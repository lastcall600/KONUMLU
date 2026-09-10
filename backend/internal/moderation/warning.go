package moderation

import (
	"context"
	"encoding/json"
	"errors"

	identitycontracts "backend/internal/identity/contracts"
	listingcontracts "backend/internal/listings/contracts"
	notifycontracts "backend/internal/notifications/contracts"
	"backend/internal/platform/outbox"
)

const warningIntentIdempotencyPrefix = "notifications.moderation.warning:"

func (s *Service) enqueueWarningIntent(ctx context.Context, row CaseAction) error {
	if s == nil || s.outbox == nil {
		return errUnavailable
	}
	ownerID, messageKey, err := s.resolveWarningRecipient(ctx, row)
	if err != nil {
		return err
	}
	intent, payload, err := warningIntentPayload(row, ownerID, messageKey)
	if err != nil {
		return err
	}
	_, err = s.outbox.Enqueue(ctx, nil, outbox.NewEvent{
		EventType:      notifycontracts.WarningEventType,
		EventVersion:   notifycontracts.WarningEventVersion,
		AggregateType:  "moderation.case_action",
		AggregateID:    row.ID.String(),
		Payload:        payload,
		IdempotencyKey: warningIntentIdempotencyPrefix + intent.IntentID,
		CorrelationID:  row.ID.String(),
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, outbox.ErrConflict) {
		return nil
	}
	return mapOutboxErr(err)
}

func (s *Service) resolveWarningRecipient(ctx context.Context, row CaseAction) (ID, string, error) {
	switch row.TargetType {
	case TargetListing:
		if s.listings == nil {
			return ID{}, "", errListingsReq
		}
		ref, err := s.listings.ResolveListingOwner(ctx, listingcontracts.ID(row.TargetID))
		if err != nil {
			return ID{}, "", mapListingOwnerErr(err)
		}
		owner := ID(ref.OwnerUserID)
		if owner.IsZero() {
			return ID{}, "", errNotFound
		}
		return owner, notifycontracts.MessageKeyWarningListing, nil
	case TargetPublicProfile:
		if s.profiles == nil {
			return ID{}, "", errProfilesReq
		}
		ownerID, err := s.profiles.ResolveUserIDByPublicID(ctx, identitycontracts.ID(row.TargetID))
		if err != nil {
			return ID{}, "", mapProfileErr(err)
		}
		owner := ID(ownerID)
		if owner.IsZero() {
			return ID{}, "", errNotFound
		}
		return owner, notifycontracts.MessageKeyWarningPublicProfile, nil
	default:
		return ID{}, "", errInvalidTarget
	}
}

func warningIntentPayload(row CaseAction, ownerID ID, messageKey string) (notifycontracts.WarningIntent, json.RawMessage, error) {
	intent := notifycontracts.WarningIntent{
		IntentID:     row.ID.String(),
		Version:      notifycontracts.WarningEventVersion,
		Purpose:      notifycontracts.PurposeTransactional,
		TemplateCode: notifycontracts.TemplateModerationWarningIssued,
		Channel:      notifycontracts.ChannelInApp,
		Locale:       notifycontracts.LocaleTR,
		Recipient: notifycontracts.RecipientRef{
			Kind: notifycontracts.RecipientUser,
			ID:   ownerID.String(),
		},
		MessageKey:    messageKey,
		TargetType:    string(row.TargetType),
		TargetRef:     row.TargetID.String(),
		ReasonCode:    string(row.ReasonCode),
		ActionAt:      row.UpdatedAt.UTC(),
		CorrelationID: row.ID.String(),
		CreatedAt:     row.UpdatedAt.UTC(),
	}
	if err := intent.Validate(); err != nil {
		return notifycontracts.WarningIntent{}, nil, errUnavailable
	}
	raw, err := json.Marshal(intent)
	if err != nil {
		return notifycontracts.WarningIntent{}, nil, errUnavailable
	}
	if _, err := notifycontracts.DecodeWarningIntent(raw); err != nil {
		return notifycontracts.WarningIntent{}, nil, errUnavailable
	}
	return intent, raw, nil
}

func mapListingOwnerErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
		return errNotFound
	}
	if errors.Is(err, listingcontracts.ErrZeroID) {
		return errZeroID
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}

func mapOutboxErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
