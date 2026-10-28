package telegram

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

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
	err = b.api.send(ctx, r.ChatID, r.Text)
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
