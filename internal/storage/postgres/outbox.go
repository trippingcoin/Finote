package postgres

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"finote/internal/assistant"
)

const enqueueReplySQL = `INSERT INTO telegram_outbox(bot_id,update_id,chat_id,response)
 VALUES($1,$2,$3,$4) ON CONFLICT(bot_id,update_id) DO NOTHING`

// Handles receipts created by versions before the outbox migration.
func (s *Store) QueueReply(ctx context.Context, bot string, update, chat int64, reply string) error {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, enqueueReplySQL, bot, update, chat, reply)
	return err
}

// Lease expiry allows recovery after a crash. Earlier pending replies keep the
// order within a chat; an unavailable chat does not block other users.
func (s *Store) NextReply(ctx context.Context, bot string) (assistant.OutgoingReply, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	var r assistant.OutgoingReply
	err := s.db.QueryRowContext(ctx, `WITH next AS (
 SELECT o.bot_id,o.update_id FROM telegram_outbox o
 WHERE o.bot_id=$1 AND o.status='pending' AND o.available_at<=CURRENT_TIMESTAMP
 AND NOT EXISTS (SELECT 1 FROM telegram_outbox older
 WHERE older.bot_id=o.bot_id AND older.chat_id=o.chat_id
 AND older.status='pending' AND older.update_id<o.update_id)
 ORDER BY o.available_at,o.update_id FOR UPDATE OF o SKIP LOCKED LIMIT 1
 ) UPDATE telegram_outbox o SET attempts=o.attempts+1,
 available_at=CURRENT_TIMESTAMP+INTERVAL '30 seconds'
 FROM next WHERE o.bot_id=next.bot_id AND o.update_id=next.update_id
 RETURNING o.update_id,o.chat_id,o.response,o.attempts`, bot).Scan(&r.UpdateID, &r.ChatID, &r.Text, &r.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return r, false, nil
	}
	return r, err == nil, err
}

func (s *Store) FinishReply(ctx context.Context, bot string, r assistant.OutgoingReply, status string, delay time.Duration, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE telegram_outbox SET status=$4,
 available_at=CURRENT_TIMESTAMP+($5 * INTERVAL '1 second'), last_error=$6
 WHERE bot_id=$1 AND update_id=$2 AND attempts=$3 AND status='pending'`,
		bot, r.UpdateID, r.Attempts, status, delay.Seconds(), reason)
	return err
}
