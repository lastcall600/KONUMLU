package messaging

import (
	"context"
	"errors"
	"time"

	listingcontracts "backend/internal/listings/contracts"
)

type Service struct {
	store    conversationStore
	listings listingcontracts.Ownership
	now      func() time.Time
}

func NewService(store conversationStore, listings listingcontracts.Ownership, now func() time.Time) (*Service, error) {
	if store == nil {
		return nil, errStoreRequired
	}
	if listings == nil {
		return nil, errListingsReq
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{store: store, listings: listings, now: now}, nil
}

func (s *Service) CreateConversation(ctx context.Context, buyerID, listingID ID) (Conversation, error) {
	if s == nil || s.store == nil {
		return Conversation{}, errStoreRequired
	}
	if buyerID.IsZero() || listingID.IsZero() {
		return Conversation{}, errZeroID
	}
	sellerID, err := s.requirePublishedOwner(ctx, listingID)
	if err != nil {
		return Conversation{}, err
	}
	if sellerID == buyerID {
		return Conversation{}, errSelfConversation
	}
	existing, err := s.store.GetByPair(ctx, listingID, buyerID, sellerID)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, errNotFound) {
		return Conversation{}, mapStoreErr(err)
	}
	id, err := NewID()
	if err != nil {
		return Conversation{}, errUnavailable
	}
	now := s.now().UTC()
	conv := Conversation{
		ID:           id,
		ListingID:    listingID,
		BuyerUserID:  buyerID,
		SellerUserID: sellerID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := conv.Validate(); err != nil {
		return Conversation{}, err
	}
	if err := s.store.InsertConversation(ctx, conv); err != nil {
		if errors.Is(err, errConflict) {
			existing, getErr := s.store.GetByPair(ctx, listingID, buyerID, sellerID)
			if getErr != nil {
				return Conversation{}, mapStoreErr(getErr)
			}
			return existing, nil
		}
		return Conversation{}, mapStoreErr(err)
	}
	return conv, nil
}

func (s *Service) GetConversation(ctx context.Context, userID, conversationID ID) (Conversation, error) {
	if s == nil || s.store == nil {
		return Conversation{}, errStoreRequired
	}
	if userID.IsZero() || conversationID.IsZero() {
		return Conversation{}, errZeroID
	}
	return s.requireParticipant(ctx, userID, conversationID)
}

func (s *Service) ListConversations(ctx context.Context, userID ID) ([]ConversationSummary, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() {
		return nil, errZeroID
	}
	rows, err := s.store.ListForUser(ctx, userID, MaxConversations)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := make([]ConversationSummary, 0, len(rows))
	for _, conv := range rows {
		summary, err := s.summarize(ctx, userID, conv)
		if err != nil {
			return nil, err
		}
		out = append(out, summary)
	}
	return out, nil
}

func (s *Service) ListMessages(ctx context.Context, userID, conversationID ID) ([]Message, error) {
	if s == nil || s.store == nil {
		return nil, errStoreRequired
	}
	if userID.IsZero() || conversationID.IsZero() {
		return nil, errZeroID
	}
	if _, err := s.requireParticipant(ctx, userID, conversationID); err != nil {
		return nil, err
	}
	msgs, err := s.store.ListRecentMessages(ctx, conversationID, MaxMessages)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return msgs, nil
}

func (s *Service) SendMessage(ctx context.Context, userID, conversationID ID, rawBody string) (Message, error) {
	if s == nil || s.store == nil {
		return Message{}, errStoreRequired
	}
	if userID.IsZero() || conversationID.IsZero() {
		return Message{}, errZeroID
	}
	body, err := NormalizeMessageBody(rawBody)
	if err != nil {
		return Message{}, err
	}
	conv, err := s.requireParticipant(ctx, userID, conversationID)
	if err != nil {
		return Message{}, err
	}
	id, err := NewID()
	if err != nil {
		return Message{}, errUnavailable
	}
	now := s.now().UTC()
	msg := Message{
		ID:             id,
		ConversationID: conv.ID,
		SenderUserID:   userID,
		Body:           body,
		CreatedAt:      now,
	}
	if err := msg.Validate(); err != nil {
		return Message{}, err
	}
	if err := s.store.InsertMessage(ctx, msg, now); err != nil {
		return Message{}, mapStoreErr(err)
	}
	lastID := msg.ID
	if err := s.store.MarkRead(ctx, conv.ID, userID, &lastID, now); err != nil {
		return Message{}, mapStoreErr(err)
	}
	return msg, nil
}

func (s *Service) MarkRead(ctx context.Context, userID, conversationID ID) error {
	if s == nil || s.store == nil {
		return errStoreRequired
	}
	if userID.IsZero() || conversationID.IsZero() {
		return errZeroID
	}
	if _, err := s.requireParticipant(ctx, userID, conversationID); err != nil {
		return err
	}
	now := s.now().UTC()
	latest, err := s.store.LatestMessage(ctx, conversationID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return mapStoreErr(s.store.MarkRead(ctx, conversationID, userID, nil, now))
		}
		return mapStoreErr(err)
	}
	lastID := latest.ID
	return mapStoreErr(s.store.MarkRead(ctx, conversationID, userID, &lastID, now))
}

func (s *Service) requirePublishedOwner(ctx context.Context, listingID ID) (ID, error) {
	if s.listings == nil {
		return ID{}, errListingsReq
	}
	ref, err := s.listings.ResolveListingOwner(ctx, listingcontracts.ID(listingID))
	if err != nil {
		if errors.Is(err, listingcontracts.ErrNotFound) || errors.Is(err, listingcontracts.ErrForbidden) {
			return ID{}, errNotFound
		}
		if errors.Is(err, listingcontracts.ErrZeroID) {
			return ID{}, errZeroID
		}
		return ID{}, errUnavailable
	}
	if !ref.PubliclyVisible() {
		return ID{}, errNotFound
	}
	if ref.OwnerUserID.IsZero() {
		return ID{}, errUnavailable
	}
	return ID(ref.OwnerUserID), nil
}

func (s *Service) requireParticipant(ctx context.Context, userID, conversationID ID) (Conversation, error) {
	conv, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return Conversation{}, errNotFound
		}
		return Conversation{}, mapStoreErr(err)
	}
	if !conv.HasParticipant(userID) {
		return Conversation{}, errNotFound
	}
	return conv, nil
}

func (s *Service) summarize(ctx context.Context, userID ID, conv Conversation) (ConversationSummary, error) {
	unread, err := s.unreadCount(ctx, userID, conv.ID)
	if err != nil {
		return ConversationSummary{}, err
	}
	summary := ConversationSummary{
		Conversation:      conv,
		CounterpartUserID: conv.Counterpart(userID),
		UnreadCount:       unread,
	}
	latest, err := s.store.LatestMessage(ctx, conv.ID)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return summary, nil
		}
		return ConversationSummary{}, mapStoreErr(err)
	}
	preview := PreviewBody(latest.Body)
	at := latest.CreatedAt.UTC()
	summary.LastMessagePreview = preview
	summary.LastMessageAt = &at
	return summary, nil
}

func (s *Service) unreadCount(ctx context.Context, userID, conversationID ID) (int, error) {
	part, err := s.store.GetParticipant(ctx, conversationID, userID)
	if err != nil {
		return 0, mapStoreErr(err)
	}
	msgs, err := s.store.ListMessages(ctx, conversationID)
	if err != nil {
		return 0, mapStoreErr(err)
	}
	var last *Message
	if part.LastReadMessageID != nil && !part.LastReadMessageID.IsZero() {
		for i := range msgs {
			if msgs[i].ID == *part.LastReadMessageID {
				copied := msgs[i]
				last = &copied
				break
			}
		}
	}
	n := 0
	for _, msg := range msgs {
		if msg.SenderUserID == userID {
			continue
		}
		if last == nil || afterRead(msg, *last) {
			n++
		}
	}
	return n, nil
}

func mapStoreErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errNotFound) || errors.Is(err, errZeroID) || errors.Is(err, errStoreRequired) ||
		errors.Is(err, errInvalidBody) || errors.Is(err, errSelfConversation) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
