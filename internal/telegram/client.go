package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// The SDK's channel poller acknowledges before handlers commit. Use GetUpdates
// explicitly, advancing offset only after the whole batch is durable.
type client struct {
	token, baseURL string
	http           *http.Client
}

func newClient(token, baseURL string, httpClient *http.Client) *client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 40 * time.Second}
	}
	return &client{token: token, baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

type apiError struct{ Code, RetryAfter int }

func (e apiError) Error() string { return fmt.Sprintf("Telegram API error %d", e.Code) }

var errWebhookConfigured = errors.New("Telegram webhook is configured; remove it before using polling")

// The SDK predates context-aware methods. Give each call a context-bound client
// without changing shared configuration or making a network call at construction.
func (c *client) sdk(ctx context.Context) *tgbotapi.BotAPI {
	bot := &tgbotapi.BotAPI{Token: c.token, Client: contextClient{ctx: ctx, client: c.http}}
	bot.SetAPIEndpoint(c.baseURL + "/bot%s/%s")
	return bot
}

type contextClient struct {
	ctx    context.Context
	client *http.Client
}

func (c contextClient) Do(request *http.Request) (*http.Response, error) {
	response, err := c.client.Do(request.WithContext(c.ctx))
	if err != nil {
		return nil, err
	}
	// Proxies may send HTML with a status code. Preserve status without decoding
	// their body or exposing Telegram URLs/tokens in error text.
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusTooManyRequests {
		response.Body.Close()
		return nil, apiError{Code: response.StatusCode}
	}
	response.Body = &limitedBody{Reader: io.LimitReader(response.Body, 4<<20), Closer: response.Body}
	return response, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}

func sanitize(err error) error {
	if err == nil {
		return nil
	}
	var api apiError
	if errors.As(err, &api) {
		return api
	}
	var sdk *tgbotapi.Error
	if errors.As(err, &sdk) {
		return apiError{Code: sdk.Code, RetryAfter: sdk.RetryAfter}
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("Telegram request failed")
}

func (c *client) updates(ctx context.Context, offset int) ([]tgbotapi.Update, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	config := tgbotapi.NewUpdate(offset)
	config.Timeout = 25
	config.AllowedUpdates = []string{"message"}
	updates, err := c.sdk(ctx).GetUpdates(config)
	return updates, sanitize(err)
}
func (c *client) webhook(ctx context.Context) (tgbotapi.WebhookInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := c.sdk(ctx).GetWebhookInfo()
	return info, sanitize(err)
}
func (c *client) send(ctx context.Context, chat int64, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.sdk(ctx).Send(tgbotapi.NewMessage(chat, text))
	return sanitize(err)
}
