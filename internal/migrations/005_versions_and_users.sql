ALTER TABLE payments ADD COLUMN version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0);

-- Every writer, including Telegram and legacy PUT/PATCH clients, advances it.
CREATE FUNCTION advance_payment_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;
CREATE TRIGGER payments_version BEFORE UPDATE ON payments
FOR EACH ROW EXECUTE FUNCTION advance_payment_version();

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    display_name TEXT NOT NULL DEFAULT '',
    username TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO users(id) SELECT DISTINCT user_id FROM payments;

-- Keep API-created members even after their last expense is deleted.
CREATE FUNCTION remember_payment_user() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO users(id) VALUES(NEW.user_id) ON CONFLICT(id) DO NOTHING;
    RETURN NEW;
END;
$$;
CREATE TRIGGER payments_user AFTER INSERT ON payments
FOR EACH ROW EXECUTE FUNCTION remember_payment_user();
