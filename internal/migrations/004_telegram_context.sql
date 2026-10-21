CREATE TABLE telegram_context (
    bot_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY(bot_id,user_id)
);
