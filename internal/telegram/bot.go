package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message"`
}
type Message struct {
	Text string `json:"text"`
	From *struct {
		ID    int64 `json:"id"`
		IsBot bool  `json:"is_bot"`
	} `json:"from"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
}

type Bot struct {
	endpoint, id string
	client       *http.Client
	planner      Planner
	store        botStore
	location     *time.Location
}

type botStore interface {
	Init(context.Context) error
	Receipt(context.Context, string, int64) (string, bool, error)
	HasDialog(context.Context, string, string) (bool, error)
	Process(context.Context, string, int64, string, Intent) (string, error)
	QueueReply(context.Context, string, int64, int64, string) error
	NextReply(context.Context, string) (outgoingReply, bool, error)
	FinishReply(context.Context, string, outgoingReply, string, time.Duration, string) error
}

func NewBot(token string, planner Planner, store *Store, location *time.Location) (*Bot, error) {
	id, _, ok := strings.Cut(token, ":")
	if _, err := strconv.ParseInt(id, 10, 64); !ok || err != nil {
		return nil, errors.New("invalid TELEGRAM_BOT_TOKEN")
	}
	return &Bot{"https://api.telegram.org/bot" + token + "/", id, &http.Client{Timeout: 40 * time.Second}, planner, store, location}, nil
}

type apiError struct{ Code, RetryAfter int }

var errWebhookConfigured = errors.New("Telegram webhook is configured; remove it before using polling")

func (e apiError) Error() string { return fmt.Sprintf("Telegram API error %d", e.Code) }

// Never return request URLs: Telegram includes the secret token in the URL.
func (b *Bot) call(ctx context.Context, method string, payload any, target any) error {
	timeout := 10 * time.Second
	if method == "getUpdates" {
		timeout = 35 * time.Second // Telegram long-polls for 25 seconds.
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint+method, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid Telegram endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := b.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Telegram request interrupted: %w", ctx.Err())
		}
		return errors.New("Telegram connection failed")
	}
	defer response.Body.Close()
	var envelope struct {
		OK         bool            `json:"ok"`
		ErrorCode  int             `json:"error_code"`
		Result     json.RawMessage `json:"result"`
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		if response.StatusCode != http.StatusOK {
			return apiError{Code: response.StatusCode}
		}
		return errors.New("invalid Telegram response")
	}
	if response.StatusCode != http.StatusOK {
		return apiError{Code: response.StatusCode, RetryAfter: envelope.Parameters.RetryAfter}
	}
	if !envelope.OK {
		return apiError{Code: envelope.ErrorCode, RetryAfter: envelope.Parameters.RetryAfter}
	}
	if target != nil {
		return json.Unmarshal(envelope.Result, target)
	}
	return nil
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.store.Init(ctx); err != nil {
		return err
	}
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
	var webhook struct {
		URL string `json:"url"`
	}
	if err := b.call(ctx, "getWebhookInfo", struct{}{}, &webhook); err != nil {
		return err
	}
	if webhook.URL != "" {
		return errWebhookConfigured
	}
	// Only acknowledge a complete batch. MAX(receipt) is unsafe when users
	// finish out of order; after restart Telegram replays unacknowledged updates.
	var offset int64
	for ctx.Err() == nil {
		var updates []Update
		err := b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &updates)
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
			if u.ID >= offset {
				offset = u.ID + 1
			}
		}
	}
	return nil
}

// Different users can progress concurrently; a user's messages stay ordered.
// Bound concurrency without acknowledging work that exists only in memory.
func (b *Bot) processBatch(ctx context.Context, updates []Update) error {
	groups := make(map[string][]Update)
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

func (b *Bot) handleUpdate(ctx context.Context, user string, u Update) error {
	reply, found, err := b.store.Receipt(ctx, b.id, u.ID)
	if err == nil {
		if found {
			err = b.store.QueueReply(ctx, b.id, u.ID, u.Message.Chat.ID, reply)
		} else {
			_, err = b.prepareReply(ctx, user, u)
		}
	}
	if err != nil {
		slog.ErrorContext(ctx, "Telegram expense operation failed", "update_id", u.ID, "error", err)
		// Keep the update unacknowledged. A retry checks its receipt before LLM.
		return fmt.Errorf("Telegram storage operation: %w", err)
	}
	return nil // reply and mutation are durable; delivery is independent
}

func (b *Bot) prepareReply(ctx context.Context, user string, u Update) (string, error) {
	text := strings.TrimSpace(u.Message.Text)
	intent := Intent{Action: "clarify", Question: "Напишите расход, например «кофе 350», или попросите показать, изменить либо удалить свои траты."}
	cancelDialog, newRequest := dialogControl(text)
	pending, err := b.store.HasDialog(ctx, b.id, user)
	if err != nil {
		return "", err
	}
	if cancelDialog || newRequest {
		intent.cancelDialog = true
		if cancelDialog {
			intent.Question = "Уточнение отменено. Ничего не изменено."
		}
	} else if pending && text != "" {
		intent.answer = text
	} else if local, ok := referenceCommand(text); ok {
		intent = local
	} else if text != "" {
		planCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		parsed, err := b.planner.Plan(planCtx, text, time.Now().In(b.location))
		cancel()
		if err != nil {
			slog.WarnContext(ctx, "Telegram: failed to parse expense request", "error", err, "update_id", u.ID)
			intent.Question = plannerErrorReply(err)
		} else {
			intent = parsed
		}
	}
	return b.store.Process(ctx, b.id, u.ID, user, intent)
}

func owner(m *Message) (string, bool) {
	if m == nil || m.From == nil || m.From.IsBot || m.From.ID <= 0 || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return "", false
	}
	return strconv.FormatInt(m.From.ID, 10), true
}
