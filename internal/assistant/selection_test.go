package assistant

import (
	"testing"
	"time"

	"finote/internal/payment/model"
)

func TestSelectionUsesConversationTimezoneAndSharedGrammar(t *testing.T) {
	loc := time.FixedZone("Almaty", 5*60*60)
	// In Almaty it is already October 29. Yesterday must mean October 28.
	now := time.Date(2026, 10, 28, 20, 30, 0, 0, time.UTC)
	choices := []model.Payment{
		{ID: "today", SpentAt: time.Date(2026, 10, 29, 0, 0, 0, 0, loc), Amount: 500},
		{ID: "yesterday", SpentAt: time.Date(2026, 10, 28, 0, 0, 0, 0, loc), Amount: 400.5},
		{ID: "third", SpentAt: time.Date(2026, 10, 27, 0, 0, 0, 0, loc), Amount: 300},
		{ID: "fourth", SpentAt: time.Date(2026, 10, 26, 0, 0, 0, 0, loc), Amount: 200},
	}
	for _, tc := range []struct{ text, want string }{
		{"вчерашнюю", "yesterday"}, {"сегодня", "today"}, {"четвёртую", "fourth"}, {"номер 3", "third"}, {"на 400,50 ₸", "yesterday"},
	} {
		selected := selectChoices(tc.text, choices, now, loc)
		if len(selected) != 1 || selected[0].ID != tc.want {
			t.Fatalf("%s: %+v", tc.text, selected)
		}
	}
}
