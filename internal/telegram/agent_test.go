package telegram

import (
	"context"
	"os"
	"strings"
	"testing"

	"finote/internal/llm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestLLMRetryAndInvalidReplyDoNotDuplicateExpenses(t *testing.T) {
	base := os.Getenv("FINOTE_LLM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_LLM_SIM_URL")
	}
	for _, mode := range []string{"retry", "invalid-json", "foreign", "401", "429"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			a, err := llm.New(base+"/"+mode, "", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			b := newTestBot(s, "", nil, a)
			u := Update{UpdateID: 1, Message: &Message{Text: t.Name(), From: &tgbotapi.User{ID: 10}, Chat: &tgbotapi.Chat{ID: 10, Type: "private"}}}
			u.Message.Chat.ID = 10
			if err = b.handleUpdate(context.Background(), "10", u); err != nil {
				t.Fatal(err)
			}
			if err = b.handleUpdate(context.Background(), "10", u); err != nil {
				t.Fatal(err)
			}
			want := 0
			if mode == "retry" {
				want = 1
			}
			assertCount(t, s, "payments", want)
			assertCount(t, s, "telegram_outbox", 1)
			reply, _, err := s.Receipt(context.Background(), "123", 1)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "401" && !strings.Contains(reply, "настроек") {
				t.Fatal(reply)
			}
			if mode == "429" && !strings.Contains(reply, "лимит") {
				t.Fatal(reply)
			}
		})
	}
}
