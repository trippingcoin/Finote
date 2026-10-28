// Package telegram adapts Telegram updates to the assistant and delivers its replies.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"finote/internal/assistant"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	id        string
	api       *client
	assistant *assistant.Service
	store     deliveryStore
}

type deliveryStore interface {
	NextReply(context.Context, string) (assistant.OutgoingReply, bool, error)
	FinishReply(context.Context, string, assistant.OutgoingReply, string, time.Duration, string) error
}

func NewBot(token string, handler *assistant.Service, delivery deliveryStore) (*Bot, error) {
	id, secret, ok := strings.Cut(token, ":")
	number, err := strconv.ParseInt(id, 10, 64)
	if !ok || err != nil || number <= 0 || secret == "" {
		return nil, errors.New("invalid TELEGRAM_BOT_TOKEN")
	}
	return &Bot{id: id, api: newClient(token, "https://api.telegram.org", nil), assistant: handler, store: delivery}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 2)
	go func() { results <- b.poll(ctx) }()
	go func() { results <- b.deliveryLoop(ctx) }()
	err := <-results
	cancel()
	other := <-results // no overlapping workers on supervisor restart
	if critical(other) {
		return other
	}
	return err
}

func (b *Bot) poll(ctx context.Context) error {
	// Do not silently delete an existing webhook.
	webhook, err := b.api.webhook(ctx)
	if err != nil {
		return err
	}
	if webhook.URL != "" {
		return errWebhookConfigured
	}
	// Only acknowledge a complete batch. MAX(receipt) is unsafe when users
	// finish out of order; after restart Telegram replays unacknowledged updates.
	var offset int
	for ctx.Err() == nil {
		updates, err := b.api.updates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var api apiError
			if errors.As(err, &api) && (api.Code == 401 || api.Code == 409) {
				return err
			}
			slog.WarnContext(ctx, "Telegram polling failed", "error", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if err := b.processBatch(ctx, updates); err != nil {
			return err // no acknowledgement: receipts make replay safe
		}
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
		}
	}
	return nil
}

// Different users can progress concurrently; a user's messages stay ordered.
// Bound concurrency without acknowledging work that exists only in memory.
func (b *Bot) processBatch(ctx context.Context, updates []tgbotapi.Update) error {
	groups := make(map[string][]tgbotapi.Update)
	for _, u := range updates {
		if user, ok := owner(u.Message); ok {
			groups[user] = append(groups[user], u)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, len(groups))
	slots := make(chan struct{}, 4)
	for user, group := range groups {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results <- ctx.Err()
				return
			}
			for _, u := range group {
				if err := b.handleUpdate(ctx, user, u); err != nil {
					results <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(results)
	var result error
	for err := range results {
		result = errors.Join(result, err)
	}
	return result
}

func (b *Bot) handleUpdate(ctx context.Context, user string, u tgbotapi.Update) error {
	verified, ok := owner(u.Message)
	if !ok || verified != user {
		return nil
	}
	profile := &assistant.Profile{
		DisplayName: truncate(strings.Join(strings.Fields(u.Message.From.FirstName+" "+u.Message.From.LastName), " "), 128),
		Username:    truncate(u.Message.From.UserName, 64),
	}
	_, err := b.assistant.Handle(ctx, assistant.Update{BotID: b.id, ID: int64(u.UpdateID), UserID: user, Profile: profile}, u.Message.Text)
	if err != nil {
		return fmt.Errorf("Telegram expense operation: %w", err)
	}
	return nil
}

func owner(m *tgbotapi.Message) (string, bool) {
	if m == nil || m.From == nil || m.Chat == nil || m.From.IsBot || m.From.ID <= 0 || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return "", false
	}
	return strconv.FormatInt(m.From.ID, 10), true
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}
