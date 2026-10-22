package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAgentHTTPFailures(t *testing.T) {
	base := os.Getenv("FINOTE_LLM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_LLM_SIM_URL to local LLM fixture")
	}
	for _, tc := range []struct {
		mode    string
		calls   int
		success bool
	}{
		{"retry", 2, true}, {"always503", 4, false}, {"401", 1, false}, {"403", 1, false}, {"404", 1, false}, {"429", 1, false}, {"500", 1, false},
		{"slow", 1, false}, {"invalid-envelope", 1, false}, {"empty", 1, false}, {"foreign", 1, false}, {"negative", 1, false}, {"action", 1, false}, {"invalid-json", 1, false}, {"extra-json", 1, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			agent, err := NewAgent(base+"/"+tc.mode, "", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			budget := 10 * time.Second
			if tc.mode == "slow" {
				budget = 150 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			started := time.Now()
			intent, err := agent.Plan(ctx, t.Name(), time.Now())
			if tc.success {
				if err != nil || intent.Action != "create" {
					t.Fatal(intent, err)
				}
			} else if err == nil {
				t.Fatal("invalid response accepted")
			}
			if tc.mode == "always503" && !errors.Is(err, ErrLLMUnavailable) {
				t.Fatal(err)
			}
			if tc.mode == "slow" && time.Since(started) > time.Second {
				t.Fatal("deadline ignored")
			}
			res, err := http.Get(base + "/counts?input=" + url.QueryEscape(t.Name()))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var stats struct{ Count int }
			if err = json.NewDecoder(res.Body).Decode(&stats); err != nil || stats.Count != tc.calls {
				t.Fatal(stats, err)
			}
		})
	}
}

func TestLLMRetryAndInvalidReplyDoNotDuplicateExpenses(t *testing.T) {
	base := os.Getenv("FINOTE_LLM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_LLM_SIM_URL")
	}
	for _, mode := range []string{"retry", "invalid-json", "foreign", "401", "429"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			a, err := NewAgent(base+"/"+mode, "", "fixture")
			if err != nil {
				t.Fatal(err)
			}
			b := &Bot{id: "123", store: s, planner: a, location: time.UTC}
			u := Update{ID: 1, Message: &Message{Text: t.Name()}}
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

func TestAgentDeadlineIncludesBackoff(t *testing.T) {
	base := os.Getenv("FINOTE_LLM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_LLM_SIM_URL")
	}
	a, err := NewAgent(base+"/always503", "", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = a.Plan(ctx, t.Name(), time.Now())
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal(err)
	}
}
