package telegram

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"finote/internal/assistant"
	"finote/internal/llm"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestRememberedList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "10")
	run := func(bot, user string, n int64, i assistant.Request) string {
		t.Helper()
		reply, err := s.Process(ctx, bot, n, user, i)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	run("123", "10", 3, assistant.Request{Command: assistant.Command{Action: "list"}})
	var raw []byte
	if err := s.db.QueryRow("SELECT payload FROM telegram_context WHERE bot_id='123' AND user_id='10'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var c assistant.ConversationContext
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	addExpense(t, s, 4, "10") // New rows must not move the remembered second entry.
	s = NewStore(s.db, time.UTC)
	run("123", "10", 5, assistant.Request{Command: assistant.Command{Action: "list", ListIndex: 2}})
	if reply := run("123", "10", 6, assistant.Request{Command: assistant.Command{Action: "id", Selected: true}}); !strings.Contains(reply, c.IDs[1]) {
		t.Fatal(reply)
	}
	amount := 700.0
	run("123", "10", 7, assistant.Request{Command: assistant.Command{Action: "update", ListIndex: 2, Amount: &amount}})
	var got float64
	if err := s.db.QueryRow("SELECT amount FROM payments WHERE id=$1", c.IDs[1]).Scan(&got); err != nil || got != 700 {
		t.Fatal(got, err)
	}
	for _, who := range [][2]string{{"123", "20"}, {"other", "10"}} {
		if reply := run(who[0], who[1], 8, assistant.Request{Command: assistant.Command{Action: "delete", ListIndex: 2}}); !strings.Contains(reply, "Контекст") {
			t.Fatal(reply)
		}
	}
	run("123", "10", 9, assistant.Request{Command: assistant.Command{Action: "delete", ListIndex: 2}})
	if reply := run("123", "10", 10, assistant.Request{Command: assistant.Command{Action: "update", ListIndex: 2, Amount: &amount}}); !strings.Contains(reply, "недоступна") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 2)
	if _, err := s.db.Exec("UPDATE telegram_context SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	if reply := run("123", "10", 11, assistant.Request{Command: assistant.Command{Action: "delete", ListIndex: 1}}); !strings.Contains(reply, "истёк") {
		t.Fatal(reply)
	}
	// Even a forged context cannot expose or delete a different owner's expense.
	if _, err := s.db.Exec("INSERT INTO telegram_context SELECT bot_id,'20',payload,CURRENT_TIMESTAMP+INTERVAL '1 minute' FROM telegram_context WHERE user_id='10'"); err != nil {
		t.Fatal(err)
	}
	if reply := run("123", "20", 12, assistant.Request{Command: assistant.Command{Action: "delete", ListIndex: 1}}); !strings.Contains(reply, "недоступна") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 2)
}

func TestDraftConversation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	category, comment := "еда", "кофе"
	run := func(n int64, i assistant.Request) string {
		t.Helper()
		reply, err := s.Process(ctx, "123", n, "10", i)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	draft := assistant.Request{Command: assistant.Command{Action: "draft_create", Category: &category, Comment: &comment}}
	run(1, draft)
	assertCount(t, s, "payments", 0)
	run(2, assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "-1"})
	assertCount(t, s, "payments", 0)
	s = NewStore(s.db, time.UTC)
	reply := run(3, assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "400,50"})
	if !strings.Contains(reply, "Расход добавлен") {
		t.Fatal(reply)
	}
	run(3, assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "400,50"})
	assertCount(t, s, "payments", 1)
	run(4, draft)
	run(5, assistant.Request{Command: assistant.Command{Action: "clarify"}, Cancel: true})
	assertCount(t, s, "telegram_context", 0)
	assertCount(t, s, "telegram_dialogs", 0)
	run(6, assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "400"})
	assertCount(t, s, "payments", 1)
	run(7, draft)
	if _, err := s.db.Exec("UPDATE telegram_dialogs SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	if reply := run(8, assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "400"}); !strings.Contains(reply, "истекло") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 1)
}

func TestAmbiguousUpdateStillResolves(t *testing.T) {
	s := testStore(t)
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "10")
	a := 700.0
	reply, err := s.Process(context.Background(), "123", 3, "10", assistant.Request{Command: assistant.Command{Action: "update", FilterCategory: "еда", Amount: &a}})
	if err != nil || !strings.Contains(reply, "несколько") {
		t.Fatal(reply, err)
	}
	reply, err = s.Process(context.Background(), "123", 4, "10", assistant.Request{Command: assistant.Command{Action: "clarify"}, Answer: "2"})
	if err != nil || !strings.Contains(reply, "обновлён") {
		t.Fatal(reply, err)
	}
	var count int
	if err = s.db.QueryRow("SELECT count(*) FROM payments WHERE amount=700").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestContextLLMSimulation(t *testing.T) {
	base := os.Getenv("FINOTE_CONTEXT_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_CONTEXT_SIM_URL to local Postman simulation")
	}
	a, err := llm.New(base, "", "test-model")
	if err != nil {
		t.Fatal(err)
	}
	for text, action := range map[string]string{"добавь кофе": "draft_create", "измени второй на 700": "update", "а id?": "id"} {
		i, err := a.Plan(context.Background(), text, time.Now())
		if err != nil || i.Action != action {
			t.Fatal(i, err)
		}
	}
	s := testStore(t)
	b := newTestBot(s, "", nil, a)
	if _, err := b.assistant.Handle(context.Background(), assistant.Update{BotID: "123", UserID: "10", ID: 1}, "добавь кофе"); err != nil {
		t.Fatal(err)
	}
	// No planner: completion and references must not depend on LLM availability.
	b.assistant = assistant.New(nil, s, time.UTC)
	for n, text := range []string{"400", "а id?", "покажи 1"} {
		if _, err := b.assistant.Handle(context.Background(), assistant.Update{BotID: "123", UserID: "10", ID: int64(n + 2)}, text); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, s, "payments", 1)
}

func TestExpenseToolsEndToEnd(t *testing.T) {
	base := os.Getenv("FINOTE_CONTEXT_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_CONTEXT_SIM_URL")
	}
	s := testStore(t)
	planner, err := llm.New(base, "", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	bot := newTestBot(s, "", nil, planner)
	ctx := context.Background()
	send := func(id int, user int64, text, want string) {
		t.Helper()
		message := &tgbotapi.Message{Text: text, From: &tgbotapi.User{ID: user}, Chat: &tgbotapi.Chat{ID: user, Type: "private"}}
		if err := bot.processBatch(ctx, []tgbotapi.Update{{UpdateID: id, Message: message}}); err != nil {
			t.Fatal(err)
		}
		reply, found, err := s.Receipt(ctx, "123", int64(id))
		if err != nil || !found || !strings.Contains(reply, want) {
			t.Fatalf("%s: reply=%s found=%v err=%v", text, reply, found, err)
		}
	}
	send(1, 10, "кофе 350", "Расход добавлен")
	send(2, 10, "такси 1200 вчера", "Расход добавлен")
	send(3, 10, "покажи траты", "такси")
	send(4, 10, "сколько потратил на еду", "350.00")
	send(5, 10, "последнее было не 1200, а 450", "Расход обновлён")
	send(5, 10, "последнее было не 1200, а 450", "Расход обновлён")
	send(6, 10, "удали такси", "Расход удалён")
	send(7, 10, "что-то", "Какое действие")
	send(8, 20, "покажи траты", "не найдено")
	send(9, 10, "а id?", "Сначала выберите")
	assertCount(t, s, "payments", 1)
	assertCount(t, s, "telegram_updates", 9)
	var amount float64
	if err := s.db.QueryRow("SELECT amount FROM payments").Scan(&amount); err != nil || amount != 350 {
		t.Fatal(amount, err)
	}
}
