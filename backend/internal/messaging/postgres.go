package messaging

import (
	"context"
	"errors"
	"time"

	"backend/internal/platform/db"
)

var _ conversationStore = (*PostgresStore)(nil)

type PostgresStore struct {
	db *db.Pool
}

func NewPostgresStore(pool *db.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

func (p *PostgresStore) InsertConversation(ctx context.Context, conv Conversation) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := conv.Validate(); err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `
		INSERT INTO messaging.conversations (
			id, listing_id, buyer_user_id, seller_user_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6)`,
		conv.ID, conv.ListingID, conv.BuyerUserID, conv.SellerUserID, conv.CreatedAt.UTC(), conv.UpdatedAt.UTC(),
	)
	if err != nil {
		if errors.Is(err, db.ErrConflict) {
			return errConflict
		}
		return mapDBErr(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO messaging.conversation_participants (conversation_id, user_id)
		VALUES ($1, $2), ($1, $3)`, conv.ID, conv.BuyerUserID, conv.SellerUserID); err != nil {
		return mapDBErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) GetConversation(ctx context.Context, id ID) (Conversation, error) {
	if p == nil || p.db == nil {
		return Conversation{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, listing_id, buyer_user_id, seller_user_id, created_at, updated_at
		FROM messaging.conversations
		WHERE id = $1`, id)
	return scanConversation(row)
}

func (p *PostgresStore) GetByPair(ctx context.Context, listingID, buyerID, sellerID ID) (Conversation, error) {
	if p == nil || p.db == nil {
		return Conversation{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, listing_id, buyer_user_id, seller_user_id, created_at, updated_at
		FROM messaging.conversations
		WHERE listing_id = $1 AND buyer_user_id = $2 AND seller_user_id = $3`,
		listingID, buyerID, sellerID)
	return scanConversation(row)
}

func (p *PostgresStore) ListForUser(ctx context.Context, userID ID, limit int) ([]Conversation, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = MaxConversations
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, listing_id, buyer_user_id, seller_user_id, created_at, updated_at
		FROM messaging.conversations
		WHERE buyer_user_id = $1 OR seller_user_id = $1
		ORDER BY updated_at DESC, id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	out := make([]Conversation, 0)
	for rows.Next() {
		conv, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, conv)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func (p *PostgresStore) InsertMessage(ctx context.Context, msg Message, conversationUpdatedAt time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	if err := msg.Validate(); err != nil {
		return err
	}
	tx, err := p.db.Begin(ctx)
	if err != nil {
		return mapDBErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		INSERT INTO messaging.messages (id, conversation_id, sender_user_id, body, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		msg.ID, msg.ConversationID, msg.SenderUserID, msg.Body, msg.CreatedAt.UTC()); err != nil {
		return mapDBErr(err)
	}
	n, err := tx.Exec(ctx, `
		UPDATE messaging.conversations
		SET updated_at = $2
		WHERE id = $1`, msg.ConversationID, conversationUpdatedAt.UTC())
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return mapDBErr(err)
	}
	return nil
}

func (p *PostgresStore) ListRecentMessages(ctx context.Context, conversationID ID, limit int) ([]Message, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	if limit <= 0 {
		limit = MaxMessages
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, conversation_id, sender_user_id, body, created_at
		FROM (
			SELECT id, conversation_id, sender_user_id, body, created_at
			FROM messaging.messages
			WHERE conversation_id = $1
			ORDER BY created_at DESC, id DESC
			LIMIT $2
		) recent
		ORDER BY created_at ASC, id ASC`, conversationID, limit)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (p *PostgresStore) LatestMessage(ctx context.Context, conversationID ID) (Message, error) {
	if p == nil || p.db == nil {
		return Message{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT id, conversation_id, sender_user_id, body, created_at
		FROM messaging.messages
		WHERE conversation_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, conversationID)
	return scanMessage(row)
}

func (p *PostgresStore) ListMessages(ctx context.Context, conversationID ID) ([]Message, error) {
	if p == nil || p.db == nil {
		return nil, errUnavailable
	}
	rows, err := p.db.Query(ctx, `
		SELECT id, conversation_id, sender_user_id, body, created_at
		FROM messaging.messages
		WHERE conversation_id = $1
		ORDER BY created_at ASC, id ASC`, conversationID)
	if err != nil {
		return nil, mapDBErr(err)
	}
	defer rows.Close()
	return scanMessages(rows)
}

func (p *PostgresStore) GetParticipant(ctx context.Context, conversationID, userID ID) (Participant, error) {
	if p == nil || p.db == nil {
		return Participant{}, errUnavailable
	}
	row := p.db.QueryRow(ctx, `
		SELECT conversation_id, user_id, last_read_message_id, last_read_at
		FROM messaging.conversation_participants
		WHERE conversation_id = $1 AND user_id = $2`, conversationID, userID)
	return scanParticipant(row)
}

func (p *PostgresStore) MarkRead(ctx context.Context, conversationID, userID ID, lastReadMessageID *ID, at time.Time) error {
	if p == nil || p.db == nil {
		return errUnavailable
	}
	n, err := p.db.Exec(ctx, `
		UPDATE messaging.conversation_participants
		SET last_read_message_id = $3, last_read_at = $4
		WHERE conversation_id = $1 AND user_id = $2`,
		conversationID, userID, lastReadMessageID, at.UTC())
	if err != nil {
		return mapDBErr(err)
	}
	if n == 0 {
		return errNotFound
	}
	return nil
}

func scanConversation(row interface {
	Scan(dest ...any) error
}) (Conversation, error) {
	var conv Conversation
	if err := row.Scan(&conv.ID, &conv.ListingID, &conv.BuyerUserID, &conv.SellerUserID, &conv.CreatedAt, &conv.UpdatedAt); err != nil {
		return Conversation{}, mapDBErr(err)
	}
	return conv, nil
}

func scanMessage(row interface {
	Scan(dest ...any) error
}) (Message, error) {
	var msg Message
	if err := row.Scan(&msg.ID, &msg.ConversationID, &msg.SenderUserID, &msg.Body, &msg.CreatedAt); err != nil {
		return Message{}, mapDBErr(err)
	}
	return msg, nil
}

func scanMessages(rows interface {
	Next() bool
	Err() error
	Scan(dest ...any) error
}) ([]Message, error) {
	out := make([]Message, 0)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, mapDBErr(err)
	}
	return out, nil
}

func scanParticipant(row interface {
	Scan(dest ...any) error
}) (Participant, error) {
	var part Participant
	var lastRead *ID
	var lastAt *time.Time
	if err := row.Scan(&part.ConversationID, &part.UserID, &lastRead, &lastAt); err != nil {
		return Participant{}, mapDBErr(err)
	}
	part.LastReadMessageID = lastRead
	part.LastReadAt = lastAt
	return part, nil
}

func mapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoRows) {
		return errNotFound
	}
	if errors.Is(err, db.ErrConflict) {
		return errConflict
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errUnavailable
}
