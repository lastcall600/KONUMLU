package messaging

import (
	"context"
	"time"
)

type conversationStore interface {
	InsertConversation(ctx context.Context, conv Conversation) error
	GetConversation(ctx context.Context, id ID) (Conversation, error)
	GetByPair(ctx context.Context, listingID, buyerID, sellerID ID) (Conversation, error)
	ListForUser(ctx context.Context, userID ID, limit int) ([]Conversation, error)
	InsertMessage(ctx context.Context, msg Message, conversationUpdatedAt time.Time) error
	ListRecentMessages(ctx context.Context, conversationID ID, limit int) ([]Message, error)
	LatestMessage(ctx context.Context, conversationID ID) (Message, error)
	ListMessages(ctx context.Context, conversationID ID) ([]Message, error)
	GetParticipant(ctx context.Context, conversationID, userID ID) (Participant, error)
	MarkRead(ctx context.Context, conversationID, userID ID, lastReadMessageID *ID, at time.Time) error
}

type Participant struct {
	ConversationID    ID
	UserID            ID
	LastReadMessageID *ID
	LastReadAt        *time.Time
}
