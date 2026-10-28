// Package postgres implements durable assistant state and delivery storage.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"finote/internal/assistant"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
)

const databaseTimeout = 5 * time.Second

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Receipt(ctx context.Context, bot string, update int64) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	var reply string
	err := s.db.QueryRowContext(ctx, "SELECT response FROM telegram_updates WHERE bot_id=$1 AND update_id=$2", bot, update).Scan(&reply)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return reply, err == nil, err
}

func (s *Store) HasDialog(ctx context.Context, bot, user string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	var found bool
	// Retain expired pending input so the user receives an explicit expiry reply.
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM telegram_dialogs WHERE bot_id=$1 AND user_id=$2)", bot, user).Scan(&found)
	return found, err
}

func (s *Store) WithUpdate(ctx context.Context, u assistant.Update, execute func(context.Context, assistant.Session) (string, error)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	user, err := service.NormalizeUserID(u.UserID)
	if err != nil {
		return "", err
	}
	chat, _ := strconv.ParseInt(user, 10, 64)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Recheck under the lock: simultaneous delivery cannot repeat a mutation.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("%s:%d", u.BotID, u.ID)); err != nil {
		return "", err
	}
	var reply string
	err = tx.QueryRowContext(ctx, `SELECT response FROM telegram_updates WHERE bot_id=$1 AND update_id=$2`, u.BotID, u.ID).Scan(&reply)
	if err == nil {
		if _, err = tx.ExecContext(ctx, enqueueReplySQL, u.BotID, u.ID, chat, reply); err != nil {
			return "", err
		}
		return reply, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "dialog:"+u.BotID+":"+user); err != nil {
		return "", err
	}
	if u.Profile != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO users(id,display_name,username) VALUES($1,$2,$3)
 ON CONFLICT(id) DO UPDATE SET display_name=EXCLUDED.display_name,username=EXCLUDED.username,updated_at=CURRENT_TIMESTAMP`, user, u.Profile.DisplayName, u.Profile.Username)
		if err != nil {
			return "", err
		}
	}
	conversation, err := readConversation(ctx, tx, u.BotID, user)
	if err != nil {
		return "", err
	}
	owned, err := repo.NewOwnedTx(tx, user)
	if err != nil {
		return "", err
	}
	reply, err = execute(ctx, assistant.Session{Payments: service.New(owned), Conversation: &conversation})
	if err != nil {
		return "", err
	}
	if err = writeConversation(ctx, tx, u.BotID, user, conversation); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_updates(bot_id,update_id,response) VALUES($1,$2,$3)`, u.BotID, u.ID, reply); err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, enqueueReplySQL, u.BotID, u.ID, chat, reply); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return reply, nil
}
