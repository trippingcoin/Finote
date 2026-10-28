package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"finote/internal/assistant"
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
		{"slow", 1, false}, {"invalid-envelope", 1, false}, {"empty", 1, false}, {"foreign", 1, false}, {"negative", 1, false}, {"action", 1, false}, {"invalid-json", 1, false}, {"extra-json", 1, false}, {"legacy", 1, false}, {"legacy-action", 1, false}, {"multiple", 1, false}, {"truncated", 1, false}, {"null", 1, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			agent, err := New(base+"/"+tc.mode, "", "fixture")
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
			if tc.mode == "always503" && !errors.Is(err, assistant.ErrPlannerUnavailable) {
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

func TestAgentDeadlineIncludesBackoff(t *testing.T) {
	base := os.Getenv("FINOTE_LLM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_LLM_SIM_URL")
	}
	a, err := New(base+"/always503", "", "fixture")
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
