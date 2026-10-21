CREATE TABLE telegram_outbox (
    bot_id TEXT NOT NULL,
    update_id BIGINT NOT NULL,
    chat_id BIGINT NOT NULL,
    response TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_error TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (bot_id, update_id),
    FOREIGN KEY (bot_id, update_id) REFERENCES telegram_updates(bot_id, update_id)
);
CREATE INDEX telegram_outbox_pending_idx ON telegram_outbox(bot_id, available_at, update_id) WHERE status='pending';
CREATE INDEX telegram_outbox_chat_idx ON telegram_outbox(bot_id, chat_id, update_id) WHERE status='pending';
