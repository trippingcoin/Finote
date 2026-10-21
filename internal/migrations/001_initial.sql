CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY,
    user_id TEXT NOT NULL,
    amount NUMERIC(14,2) NOT NULL CHECK (amount > 0),
    category TEXT NOT NULL,
    spent_at TIMESTAMPTZ NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS telegram_updates (
    bot_id TEXT NOT NULL,
    update_id BIGINT NOT NULL,
    response TEXT NOT NULL,
    PRIMARY KEY (bot_id, update_id)
);

CREATE TABLE IF NOT EXISTS telegram_dialogs (
    bot_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    payload JSONB NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (bot_id, user_id)
);
