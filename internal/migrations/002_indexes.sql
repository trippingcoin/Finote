CREATE INDEX IF NOT EXISTS payments_owner_spent_idx ON payments (user_id, spent_at DESC);
CREATE INDEX IF NOT EXISTS payments_owner_created_idx ON payments (user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS payments_spent_idx ON payments (spent_at DESC, created_at DESC, id DESC);
