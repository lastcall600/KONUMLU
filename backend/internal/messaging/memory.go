package messaging

import (
	"context"
	"sort"
	"sync"
	"time"
)

type memoryPair struct {
	listing ID
	buyer   ID
	seller  ID
}

// MemoryStore is an in-process messaging store for tests.
type MemoryStore struct {
	mu           sync.Mutex
	byID         map[ID]Conversation
	byPair       map[memoryPair]ID
	messages     map[ID][]Message
	participants map[ID]map[ID]Participant
	fail         error
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:         make(map[ID]Conversation),
		byPair:       make(map[memoryPair]ID),
		messages:     make(map[ID][]Message),
		participants: make(map[ID]map[ID]Participant),
	}
}

func (m *MemoryStore) SetFail(err error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fail = err
}

func (m *MemoryStore) InsertConversation(ctx context.Context, conv Conversation) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := conv.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	pair := memoryPair{listing: conv.ListingID, buyer: conv.BuyerUserID, seller: conv.SellerUserID}
	if _, ok := m.byPair[pair]; ok {
		return errConflict
	}
	m.byID[conv.ID] = conv
	m.byPair[pair] = conv.ID
	m.messages[conv.ID] = nil
	parts := map[ID]Participant{
		conv.BuyerUserID:  {ConversationID: conv.ID, UserID: conv.BuyerUserID},
		conv.SellerUserID: {ConversationID: conv.ID, UserID: conv.SellerUserID},
	}
	m.participants[conv.ID] = parts
	return nil
}

func (m *MemoryStore) GetConversation(ctx context.Context, id ID) (Conversation, error) {
	if m == nil {
		return Conversation{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Conversation{}, m.fail
	}
	conv, ok := m.byID[id]
	if !ok {
		return Conversation{}, errNotFound
	}
	return conv, nil
}

func (m *MemoryStore) GetByPair(ctx context.Context, listingID, buyerID, sellerID ID) (Conversation, error) {
	if m == nil {
		return Conversation{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Conversation{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Conversation{}, m.fail
	}
	id, ok := m.byPair[memoryPair{listing: listingID, buyer: buyerID, seller: sellerID}]
	if !ok {
		return Conversation{}, errNotFound
	}
	return m.byID[id], nil
}

func (m *MemoryStore) ListForUser(ctx context.Context, userID ID, limit int) ([]Conversation, error) {
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
	out := make([]Conversation, 0)
	for _, conv := range m.byID {
		if conv.HasParticipant(userID) {
			out = append(out, conv)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID.String() > out[j].ID.String()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) InsertMessage(ctx context.Context, msg Message, conversationUpdatedAt time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	conv, ok := m.byID[msg.ConversationID]
	if !ok {
		return errNotFound
	}
	m.messages[msg.ConversationID] = append(m.messages[msg.ConversationID], msg)
	conv.UpdatedAt = conversationUpdatedAt.UTC()
	m.byID[msg.ConversationID] = conv
	return nil
}

func (m *MemoryStore) ListRecentMessages(ctx context.Context, conversationID ID, limit int) ([]Message, error) {
	all, err := m.ListMessages(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

func (m *MemoryStore) LatestMessage(ctx context.Context, conversationID ID) (Message, error) {
	all, err := m.ListMessages(ctx, conversationID)
	if err != nil {
		return Message{}, err
	}
	if len(all) == 0 {
		return Message{}, errNotFound
	}
	return all[len(all)-1], nil
}

func (m *MemoryStore) ListMessages(ctx context.Context, conversationID ID) ([]Message, error) {
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
	if _, ok := m.byID[conversationID]; !ok {
		return nil, errNotFound
	}
	src := m.messages[conversationID]
	out := make([]Message, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID.String() < out[j].ID.String()
	})
	return out, nil
}

func (m *MemoryStore) GetParticipant(ctx context.Context, conversationID, userID ID) (Participant, error) {
	if m == nil {
		return Participant{}, errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return Participant{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return Participant{}, m.fail
	}
	parts, ok := m.participants[conversationID]
	if !ok {
		return Participant{}, errNotFound
	}
	part, ok := parts[userID]
	if !ok {
		return Participant{}, errNotFound
	}
	return part, nil
}

func (m *MemoryStore) MarkRead(ctx context.Context, conversationID, userID ID, lastReadMessageID *ID, at time.Time) error {
	if m == nil {
		return errStoreRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	parts, ok := m.participants[conversationID]
	if !ok {
		return errNotFound
	}
	part, ok := parts[userID]
	if !ok {
		return errNotFound
	}
	readAt := at.UTC()
	part.LastReadAt = &readAt
	part.LastReadMessageID = lastReadMessageID
	parts[userID] = part
	return nil
}
