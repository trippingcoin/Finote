package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"finote/internal/assistant"
)

// Preserve existing tables and JSON payloads during the package refactor.
func readConversation(ctx context.Context, tx *sql.Tx, bot, user string) (assistant.Conversation, error) {
	var c assistant.Conversation
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT payload,expires_at FROM telegram_context WHERE bot_id=$1 AND user_id=$2", bot, user).Scan(&raw, &c.ContextExpiresAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return c, err
	}
	if err == nil {
		if err = json.Unmarshal(raw, &c.Context); err != nil {
			return c, err
		}
	}
	err = tx.QueryRowContext(ctx, "SELECT payload,expires_at FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2", bot, user).Scan(&raw, &c.PendingExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Pending = &assistant.PendingDialog{}
	return c, json.Unmarshal(raw, c.Pending)
}

func writeConversation(ctx context.Context, tx *sql.Tx, bot, user string, c assistant.Conversation) error {
	if c.ContextExpiresAt.IsZero() {
		if _, err := tx.ExecContext(ctx, "DELETE FROM telegram_context WHERE bot_id=$1 AND user_id=$2", bot, user); err != nil {
			return err
		}
	} else {
		raw, err := json.Marshal(c.Context)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_context(bot_id,user_id,payload,expires_at) VALUES($1,$2,$3,$4)
 ON CONFLICT(bot_id,user_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, bot, user, string(raw), c.ContextExpiresAt); err != nil {
			return err
		}
	}
	if c.Pending == nil {
		_, err := tx.ExecContext(ctx, "DELETE FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2", bot, user)
		return err
	}
	raw, err := json.Marshal(c.Pending)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_dialogs(bot_id,user_id,payload,expires_at) VALUES($1,$2,$3,$4)
 ON CONFLICT(bot_id,user_id) DO UPDATE SET payload=EXCLUDED.payload,expires_at=EXCLUDED.expires_at`, bot, user, string(raw), c.PendingExpiresAt)
	return err
}
