package telegram

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestOutboxSimulation(t *testing.T) {
	base := os.Getenv("FINOTE_TELEGRAM_SIM_URL")
	if base == "" {
		t.Skip("set FINOTE_TELEGRAM_SIM_URL for the local Postman simulation")
	}
	s := testStore(t)
	addExpense(t, s, 1, "10")
	b := newTestBot(s, base+"/error/", &http.Client{Timeout: time.Second}, nil)
	if _, err := b.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "pending")
	makeDue(t, s)
	b = newTestBot(NewStore(s.db, time.UTC), base+"/ok/", &http.Client{Timeout: time.Second}, nil)
	if _, err := b.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "sent")
	assertCount(t, s, "payments", 1)
}
