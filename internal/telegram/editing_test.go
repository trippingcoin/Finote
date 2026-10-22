package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"finote/internal/payment/httpapi"
	"finote/internal/payment/model"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
)

func TestPanelConflictWithTelegramAndConcurrentEditors(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	server := httptest.NewServer(httpapi.New(service.New(repo.New(s.db))))
	defer server.Close()
	request := func(method, path, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var raw json.RawMessage
		if res.StatusCode != 204 {
			if err = json.NewDecoder(res.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
		}
		return res.StatusCode, raw
	}
	status, raw := request("POST", "/v1/payments", `{"user_id":"10","amount":400,"category":"еда","comment":"кофе"}`)
	var p model.Payment
	if err := json.Unmarshal(raw, &p); err != nil || status != 201 || p.Version != 1 {
		t.Fatal(status, string(raw), err)
	}
	amount := 500.0
	if _, err := s.Process(ctx, "123", 1, "10", Intent{Action: "update", ID: p.ID, Amount: &amount}); err != nil {
		t.Fatal(err)
	}
	path := "/v1/payments/" + p.ID
	status, raw = request("PATCH", path, `{"comment":"с собой","expected_version":1}`)
	if status != 409 || !strings.Contains(string(raw), "version_conflict") {
		t.Fatal(status, string(raw))
	}
	status, raw = request("GET", path, "")
	json.Unmarshal(raw, &p)
	if status != 200 || p.Amount != 500 || p.Comment != "кофе" || p.Version != 2 {
		t.Fatal(p)
	}
	status, raw = request("PATCH", path, `{"comment":"с собой","expected_version":2}`)
	json.Unmarshal(raw, &p)
	if status != 200 || p.Amount != 500 || p.Comment != "с собой" || p.Version != 3 {
		t.Fatal(status, p)
	}
	status, _ = request("PATCH", path, `{"comment":"bad","expected_version":0}`)
	if status != 400 {
		t.Fatal(status)
	}
	// A stale owner-bound operation must not reveal or modify someone else's row.
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	owned, err := repo.NewOwnedTx(tx, "20")
	if err != nil {
		t.Fatal(err)
	}
	v := int64(1)
	comment := "foreign"
	if _, err = service.New(owned).PatchPayment(ctx, p.ID, model.UpdatePayment{Comment: &comment, ExpectedVersion: &v}); err == nil || err == model.ErrConflict {
		t.Fatal("owner leak", err)
	}
	tx.Rollback()
	// Two editors use the same version: exactly one wins.
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := request("PATCH", path, `{"comment":"parallel","expected_version":3}`)
			statuses <- status
		}()
	}
	wg.Wait()
	close(statuses)
	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
	// Legacy writes still advance the version.
	status, raw = request("PATCH", path, `{"amount":600}`)
	json.Unmarshal(raw, &p)
	if status != 200 || p.Version != 5 {
		t.Fatal(status, p)
	}
	status, raw = request("GET", "/v1/payments", "")
	var list model.PaymentList
	json.Unmarshal(raw, &list)
	if len(list.Items) != 1 || list.Items[0].Version != 5 {
		t.Fatal(string(raw))
	}
	request("DELETE", path, "")
	status, _ = request("PATCH", path, `{"comment":"deleted","expected_version":5}`)
	if status != 404 {
		t.Fatal(status)
	}
	status, raw = request("GET", "/v1/users", "")
	if status != 200 || string(raw) != `["10"]` {
		t.Fatal(status, string(raw))
	}
	status, raw = request("GET", "/v1/users?details=true", "")
	var users []model.User
	json.Unmarshal(raw, &users)
	if status != 200 || len(users) != 1 || users[0].ID != "10" {
		t.Fatal(string(raw))
	}
}

func TestTelegramProfileStoredOnStartAndUpdated(t *testing.T) {
	s := testStore(t)
	b := &Bot{id: "123", store: s, location: time.UTC}
	update := func(n int64, name, username string) Update {
		var u Update
		raw := fmt.Sprintf(`{"update_id":%d,"message":{"text":"/start","from":{"id":10,"is_bot":false,"first_name":%q,"last_name":"Family","username":%q},"chat":{"id":10,"type":"private"}}}`, n, name, username)
		if err := json.Unmarshal([]byte(raw), &u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	first := update(1, "Абылай", "abylay")
	if err := b.handleUpdate(context.Background(), "10", first); err != nil {
		t.Fatal(err)
	}
	users, err := repo.New(s.db).ListUserProfiles(context.Background())
	if err != nil || len(users) != 1 || users[0].DisplayName != "Абылай Family" || users[0].Username != "abylay" {
		t.Fatal(users, err)
	}
	assertCount(t, s, "payments", 0)
	if err := b.handleUpdate(context.Background(), "10", update(2, "Новое имя", "")); err != nil {
		t.Fatal(err)
	}
	if err := b.handleUpdate(context.Background(), "10", first); err != nil {
		t.Fatal(err)
	} // replay cannot revert profile
	users, err = repo.New(s.db).ListUserProfiles(context.Background())
	if err != nil || users[0].DisplayName != "Новое имя Family" || users[0].Username != "" {
		t.Fatal(users, err)
	}
	forged := update(3, "Wrong", "wrong")
	forged.Message.Chat.ID = 20
	if _, err = b.prepareReply(context.Background(), "10", forged); err != nil {
		t.Fatal(err)
	}
	users, _ = repo.New(s.db).ListUserProfiles(context.Background())
	if users[0].DisplayName != "Новое имя Family" {
		t.Fatal(users)
	}
	// A rollback must not persist a changed profile.
	if _, err = s.db.Exec("ALTER TABLE telegram_outbox ADD CONSTRAINT reject_five CHECK(update_id<>5)"); err != nil {
		t.Fatal(err)
	}
	if _, err = b.prepareReply(context.Background(), "10", update(5, "rollback", "")); err == nil {
		t.Fatal("expected rollback")
	}
	users, _ = repo.New(s.db).ListUserProfiles(context.Background())
	if users[0].DisplayName != "Новое имя Family" {
		t.Fatal(users)
	}
	if err = s.Init(context.Background()); err != nil {
		t.Fatal("migration rerun", err)
	}
}
