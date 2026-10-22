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
	b := &Bot{endpoint: base + "/error/", id: "123", client: &http.Client{Timeout: time.Second}, store: s}
	if _, err := b.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "pending")
	makeDue(t, s)
	b = &Bot{endpoint: base + "/ok/", id: "123", client: &http.Client{Timeout: time.Second}, store: NewStore(s.db, time.UTC)}
	if _, err := b.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "sent")
	assertCount(t, s, "payments", 1)
}
