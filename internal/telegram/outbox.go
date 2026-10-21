package telegram

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"
)

type outgoingReply struct {
	UpdateID, ChatID int64
	Text             string
	Attempts         int
}

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
func (s *Store) NextReply(ctx context.Context, bot string) (outgoingReply, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	var r outgoingReply
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

func (s *Store) FinishReply(ctx context.Context, bot string, r outgoingReply, status string, delay time.Duration, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, databaseTimeout)
	defer cancel()
	_, err := s.db.ExecContext(ctx, `UPDATE telegram_outbox SET status=$4,
 available_at=CURRENT_TIMESTAMP+($5 * INTERVAL '1 second'), last_error=$6
 WHERE bot_id=$1 AND update_id=$2 AND attempts=$3 AND status='pending'`,
		bot, r.UpdateID, r.Attempts, status, delay.Seconds(), reason)
	return err
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func critical(err error) bool {
	var api apiError
	return errors.Is(err, errWebhookConfigured) || (errors.As(err, &api) && (api.Code == 401 || api.Code == 409))
}

// RunIsolated deliberately never forwards Telegram errors to the HTTP server.
func RunIsolated(ctx context.Context, run func(context.Context) error) {
	for ctx.Err() == nil {
		err := run(ctx)
		if ctx.Err() != nil {
			return
		}
		if critical(err) {
			slog.ErrorContext(ctx, "Telegram stopped; web panel remains available", "error", err)
			return
		}
		slog.WarnContext(ctx, "Telegram restarting after temporary failure", "error", err)
		if !wait(ctx, 3*time.Second) {
			return
		}
	}
}

func (b *Bot) deliverNext(ctx context.Context) (bool, error) {
	r, found, err := b.store.NextReply(ctx, b.id)
	if err != nil || !found {
		return found, err
	}
	err = b.call(ctx, "sendMessage", map[string]any{"chat_id": r.ChatID, "text": r.Text}, nil)
	status, reason := "sent", ""
	delay := time.Duration(1<<min(r.Attempts, 8)) * time.Second
	if err != nil {
		status, reason = "pending", err.Error()
		var api apiError
		if errors.As(err, &api) {
			if api.Code == 400 || api.Code == 403 {
				status = "failed"
			}
			if api.RetryAfter > 0 {
				delay = max(delay, time.Duration(api.RetryAfter)*time.Second)
			}
		}
		slog.WarnContext(ctx, "Telegram reply delivery failed", "update_id", r.UpdateID, "attempt", r.Attempts, "status", status, "error", err)
	}
	if saveErr := b.store.FinishReply(ctx, b.id, r, status, delay, reason); saveErr != nil {
		return true, saveErr
	}
	if critical(err) {
		return true, err
	}
	return true, nil
}

func (b *Bot) deliveryLoop(ctx context.Context) error {
	for ctx.Err() == nil {
		found, err := b.deliverNext(ctx)
		if critical(err) {
			return err
		}
		if err != nil {
			slog.WarnContext(ctx, "Telegram outbox unavailable", "error", err)
		}
		if err != nil || !found {
			if !wait(ctx, time.Second) {
				return nil
			}
		}
	}
	return nil
}
