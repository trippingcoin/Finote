package telegram

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestReferenceGrammar(t *testing.T) {
	for _, text := range []string{"покажи 1", "покажи первое в списке", "измени второй на 700", "а id?", "удали второй"} {
		i, ok := referenceCommand(text)
		if !ok || validate(i) != nil {
			t.Fatalf("%s: %+v", text, i)
		}
	}
	for _, text := range []string{"кофе 400", "удали второй и первый", "покажи список"} {
		if _, ok := referenceCommand(text); ok {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"400", "400,50", "700 ₸"} {
		if _, ok := parseAmount(text); !ok {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"-1", "NaN", "400 кофе", "4.555"} {
		if _, ok := parseAmount(text); ok {
			t.Fatal(text)
		}
	}
}

func TestRememberedList(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "10")
	run := func(bot, user string, n int64, i Intent) string {
		t.Helper()
		reply, err := s.Process(ctx, bot, n, user, i)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	run("123", "10", 3, Intent{Action: "list"})
	var raw []byte
	if err := s.db.QueryRow("SELECT payload FROM telegram_context WHERE bot_id='123' AND user_id='10'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var c conversationContext
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	addExpense(t, s, 4, "10") // New rows must not move the remembered second entry.
	s = NewStore(s.db, time.UTC)
	run("123", "10", 5, Intent{Action: "list", ListIndex: 2})
	if reply := run("123", "10", 6, Intent{Action: "id", Selected: true}); !strings.Contains(reply, c.IDs[1]) {
		t.Fatal(reply)
	}
	amount := 700.0
	run("123", "10", 7, Intent{Action: "update", ListIndex: 2, Amount: &amount})
	var got float64
	if err := s.db.QueryRow("SELECT amount FROM payments WHERE id=$1", c.IDs[1]).Scan(&got); err != nil || got != 700 {
		t.Fatal(got, err)
	}
	for _, who := range [][2]string{{"123", "20"}, {"other", "10"}} {
		if reply := run(who[0], who[1], 8, Intent{Action: "delete", ListIndex: 2}); !strings.Contains(reply, "Контекст") {
			t.Fatal(reply)
		}
	}
	run("123", "10", 9, Intent{Action: "delete", ListIndex: 2})
	if reply := run("123", "10", 10, Intent{Action: "update", ListIndex: 2, Amount: &amount}); !strings.Contains(reply, "недоступна") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 2)
	if _, err := s.db.Exec("UPDATE telegram_context SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	if reply := run("123", "10", 11, Intent{Action: "delete", ListIndex: 1}); !strings.Contains(reply, "истёк") {
		t.Fatal(reply)
	}
	// Even a forged context cannot expose or delete a different owner's expense.
	if _, err := s.db.Exec("INSERT INTO telegram_context SELECT bot_id,'20',payload,CURRENT_TIMESTAMP+INTERVAL '1 minute' FROM telegram_context WHERE user_id='10'"); err != nil {
		t.Fatal(err)
	}
	if reply := run("123", "20", 12, Intent{Action: "delete", ListIndex: 1}); !strings.Contains(reply, "недоступна") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 2)
}

func TestDraftConversation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	category, comment := "еда", "кофе"
	run := func(n int64, i Intent) string {
		t.Helper()
		reply, err := s.Process(ctx, "123", n, "10", i)
		if err != nil {
			t.Fatal(err)
		}
		return reply
	}
	draft := Intent{Action: "draft_create", Category: &category, Comment: &comment}
	run(1, draft)
	assertCount(t, s, "payments", 0)
	run(2, Intent{Action: "clarify", answer: "-1"})
	assertCount(t, s, "payments", 0)
	s = NewStore(s.db, time.UTC)
	reply := run(3, Intent{Action: "clarify", answer: "400,50"})
	if !strings.Contains(reply, "Расход добавлен") {
		t.Fatal(reply)
	}
	run(3, Intent{Action: "clarify", answer: "400,50"})
	assertCount(t, s, "payments", 1)
	run(4, draft)
	run(5, Intent{Action: "clarify", cancelDialog: true})
	assertCount(t, s, "telegram_context", 0)
	assertCount(t, s, "telegram_dialogs", 0)
	run(6, Intent{Action: "clarify", answer: "400"})
	assertCount(t, s, "payments", 1)
	run(7, draft)
	if _, err := s.db.Exec("UPDATE telegram_dialogs SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'"); err != nil {
		t.Fatal(err)
	}
	if reply := run(8, Intent{Action: "clarify", answer: "400"}); !strings.Contains(reply, "истекло") {
		t.Fatal(reply)
	}
	assertCount(t, s, "payments", 1)
}

func TestAmbiguousUpdateStillResolves(t *testing.T) {
	s := testStore(t)
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "10")
	a := 700.0
	reply, err := s.Process(context.Background(), "123", 3, "10", Intent{Action: "update", FilterCategory: "еда", Amount: &a})
	if err != nil || !strings.Contains(reply, "несколько") {
		t.Fatal(reply, err)
	}
	reply, err = s.Process(context.Background(), "123", 4, "10", Intent{Action: "clarify", answer: "2"})
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
	a, err := NewAgent(base, "", "test-model")
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
	b := &Bot{id: "123", store: s, planner: a, location: time.UTC}
	if _, err := b.prepareReply(context.Background(), "10", Update{ID: 1, Message: &Message{Text: "добавь кофе"}}); err != nil {
		t.Fatal(err)
	}
	// No planner: completion and references must not depend on LLM availability.
	b.planner = nil
	for n, text := range []string{"400", "а id?", "покажи 1"} {
		if _, err := b.prepareReply(context.Background(), "10", Update{ID: int64(n + 2), Message: &Message{Text: text}}); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, s, "payments", 1)
}
