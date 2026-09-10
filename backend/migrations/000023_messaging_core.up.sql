CREATE SCHEMA messaging;

-- UUID references only. No FK to identity or listings tables.
CREATE TABLE messaging.conversations (
    id UUID PRIMARY KEY,
    listing_id UUID NOT NULL,
    buyer_user_id UUID NOT NULL,
    seller_user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT conversations_buyer_seller_distinct CHECK (buyer_user_id <> seller_user_id),
    CONSTRAINT conversations_listing_pair_unique UNIQUE (listing_id, buyer_user_id, seller_user_id)
);

CREATE TABLE messaging.messages (
    id UUID PRIMARY KEY,
    conversation_id UUID NOT NULL REFERENCES messaging.conversations (id),
    sender_user_id UUID NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE messaging.conversation_participants (
    conversation_id UUID NOT NULL REFERENCES messaging.conversations (id),
    user_id UUID NOT NULL,
    last_read_message_id UUID REFERENCES messaging.messages (id),
    last_read_at TIMESTAMPTZ,
    PRIMARY KEY (conversation_id, user_id)
);

CREATE INDEX conversations_buyer_updated_idx
    ON messaging.conversations (buyer_user_id, updated_at DESC, id DESC);

CREATE INDEX conversations_seller_updated_idx
    ON messaging.conversations (seller_user_id, updated_at DESC, id DESC);

CREATE INDEX messages_conversation_created_idx
    ON messaging.messages (conversation_id, created_at ASC, id ASC);
