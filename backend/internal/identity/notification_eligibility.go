package identity

import (
	"context"
	"errors"

	"backend/internal/identity/contracts"
)

// NotificationEligibilityReader is the Identity-owned adapter for Notifications.
type NotificationEligibilityReader struct {
	store identifierStore
}

func NewNotificationEligibilityReader(store identifierStore) (*NotificationEligibilityReader, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	return &NotificationEligibilityReader{store: store}, nil
}

func (r *NotificationEligibilityReader) ReadNotificationEligibility(ctx context.Context, userID contracts.ID) (contracts.NotificationEligibility, error) {
	if r == nil || r.store == nil {
		return contracts.NotificationEligibility{}, errUnavailable
	}
	var id ID
	copy(id[:], userID[:])
	if id.IsZero() {
		return contracts.NotificationEligibility{}, errZeroID
	}
	user, err := r.store.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return contracts.NotificationEligibility{}, contracts.ErrNotFound
		}
		return contracts.NotificationEligibility{}, contracts.ErrUnavailable
	}
	out := contracts.NotificationEligibility{
		Deleted:  user.DeletedAt != nil,
		Disabled: user.DisabledAt != nil,
	}
	idents, err := r.store.ListActiveIdentifiersForUser(ctx, id)
	if err != nil {
		return contracts.NotificationEligibility{}, contracts.ErrUnavailable
	}
	for _, ident := range idents {
		if !ident.VerifiedActive() {
			continue
		}
		switch ident.Kind {
		case IdentifierEmail:
			out.EmailVerified = true
		case IdentifierPhone:
			out.PhoneVerified = true
		}
	}
	return out, nil
}

func (r *NotificationEligibilityReader) ResolveVerifiedContact(ctx context.Context, userID contracts.ID, kind string) (contracts.NotificationContact, error) {
	if r == nil || r.store == nil {
		return contracts.NotificationContact{}, errUnavailable
	}
	var id ID
	copy(id[:], userID[:])
	if id.IsZero() {
		return contracts.NotificationContact{}, errZeroID
	}
	want := IdentifierEmail
	switch kind {
	case contracts.NotificationContactEmail:
		want = IdentifierEmail
	case contracts.NotificationContactPhone:
		want = IdentifierPhone
	default:
		return contracts.NotificationContact{}, contracts.ErrNotFound
	}
	user, err := r.store.GetUser(ctx, id)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return contracts.NotificationContact{}, contracts.ErrNotFound
		}
		return contracts.NotificationContact{}, contracts.ErrUnavailable
	}
	if user.DeletedAt != nil {
		return contracts.NotificationContact{}, contracts.ErrNotFound
	}
	idents, err := r.store.ListActiveIdentifiersForUser(ctx, id)
	if err != nil {
		return contracts.NotificationContact{}, contracts.ErrUnavailable
	}
	for _, ident := range idents {
		if ident.Kind != want || !ident.VerifiedActive() {
			continue
		}
		return contracts.NotificationContact{Kind: kind, Value: ident.ValueCanonical}, nil
	}
	return contracts.NotificationContact{}, contracts.ErrNotFound
}
