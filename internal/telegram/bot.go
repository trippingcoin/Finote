package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
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
	store        *Store
	location     *time.Location
}

func NewBot(token string, planner Planner, store *Store, location *time.Location) (*Bot, error) {
	id, _, ok := strings.Cut(token, ":")
	if _, err := strconv.ParseInt(id, 10, 64); !ok || err != nil {
		return nil, errors.New("invalid TELEGRAM_BOT_TOKEN")
	}
	return &Bot{"https://api.telegram.org/bot" + token + "/", id, &http.Client{Timeout: 40 * time.Second}, planner, store, location}, nil
}

type apiError struct{ Code int }

func (e apiError) Error() string { return fmt.Sprintf("Telegram API error %d", e.Code) }

// Never return request URLs: Telegram includes the secret token in the URL.
func (b *Bot) call(ctx context.Context, method string, payload any, target any) error {
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
		return errors.New("Telegram connection failed")
	}
	defer response.Body.Close()
	var envelope struct {
		OK        bool            `json:"ok"`
		ErrorCode int             `json:"error_code"`
		Result    json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&envelope); err != nil {
		return errors.New("invalid Telegram response")
	}
	if !envelope.OK {
		return apiError{envelope.ErrorCode}
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
	// Do not silently delete an existing webhook.
	var webhook struct {
		URL string `json:"url"`
	}
	if err := b.call(ctx, "getWebhookInfo", struct{}{}, &webhook); err != nil {
		return err
	}
	if webhook.URL != "" {
		return errors.New("Telegram webhook is configured; remove it before using polling")
	}
	offset, err := b.store.Offset(ctx, b.id)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		var updates []Update
		err = b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var api apiError
			if errors.As(err, &api) && (api.Code == 401 || api.Code == 409) {
				return err
			}
			log.Printf("Telegram polling: %v", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if u.ID < offset {
				continue
			}
			user, ok := owner(u.Message)
			if ok {
				text := strings.TrimSpace(u.Message.Text)
				intent := Intent{Action: "clarify", Question: "Напишите расход, например «кофе 350», или попросите показать, изменить либо удалить свои траты."}
				if text != "" && text != "/start" {
					planCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
					parsed, err := b.planner.Plan(planCtx, text, time.Now().In(b.location))
					cancel()
					if err != nil {
						log.Printf("Telegram: failed to parse expense request: %v", err)
						intent.Question = "Не удалось разобрать запрос. Укажите действие, сумму и описание; если ошибка повторяется, попробуйте позже."
					} else {
						intent = parsed
					}
				}
				reply, err := b.store.Process(ctx, b.id, u.ID, user, intent)
				if err != nil {
					return fmt.Errorf("Telegram expense operation: %w", err)
				}
				// Retry delivery only, never the expense mutation.
				for {
					err = b.call(ctx, "sendMessage", map[string]any{"chat_id": u.Message.Chat.ID, "text": reply}, nil)
					if err == nil {
						break
					}
					if ctx.Err() != nil {
						return nil
					}
					var api apiError
					if errors.As(err, &api) && (api.Code == 400 || api.Code == 403) {
						break
					}
					if errors.As(err, &api) && api.Code == 401 {
						return err
					}
					log.Print("Telegram: retrying reply delivery")
					select {
					case <-ctx.Done():
						return nil
					case <-time.After(3 * time.Second):
					}
				}
			}
			offset = u.ID + 1
		}
	}
	return nil
}

func owner(m *Message) (string, bool) {
	if m == nil || m.From == nil || m.From.IsBot || m.From.ID <= 0 || m.Chat.Type != "private" || m.Chat.ID != m.From.ID {
		return "", false
	}
	return strconv.FormatInt(m.From.ID, 10), true
}
