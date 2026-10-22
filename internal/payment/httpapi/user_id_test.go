package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"finote/internal/migrations"
	"finote/internal/payment/model"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	_ "github.com/lib/pq"
)

func TestCanonicalOwnerAcrossAPIAndBot(t *testing.T) {
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set isolated FINOTE_TEST_DATABASE_URL")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("owner_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	defer admin.Close()
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(service.New(repo.New(db))))
	defer server.Close()
	request := func(method, path, body string, status int, target any) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("%s %s: %d want %d", method, path, res.StatusCode, status)
		}
		if target != nil {
			if err = json.NewDecoder(res.Body).Decode(target); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []string{"123", "00123", "+123", "  +00123  "} {
		var p model.Payment
		request("POST", "/v1/payments", fmt.Sprintf(`{"user_id":%q,"amount":400,"category":"еда"}`, id), 201, &p)
		if p.UserID != "123" {
			t.Fatalf("owner=%q", p.UserID)
		}
		var saved string
		if err = db.QueryRow("SELECT user_id FROM payments WHERE id=$1", p.ID).Scan(&saved); err != nil || saved != "123" {
			t.Fatalf("stored=%q %v", saved, err)
		}
	}
	for _, id := range []string{"123", "00123", "+123", " 123 "} {
		var result model.PaymentList
		request("GET", "/v1/payments?user_id="+url.QueryEscape(id), "", 200, &result)
		if result.Total != 4 || result.Members != 1 {
			t.Fatalf("filter=%q result=%+v", id, result)
		}
	}
	var users []string
	request("GET", "/v1/users", "", 200, &users)
	if len(users) != 1 || users[0] != "123" {
		t.Fatalf("users=%v", users)
	}
	for _, id := range []string{"abc", "0", "-123", " ", "9223372036854775808"} {
		request("POST", "/v1/payments", fmt.Sprintf(`{"user_id":%q,"amount":400,"category":"еда"}`, id), 400, nil)
		request("GET", "/v1/payments?user_id="+url.QueryEscape(id), "", 400, nil)
	}
	// Same owner-bound repository used by the bot: only the canonical owner sees them.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, owner := range []string{"123", "456"} {
		owned, err := repo.NewOwnedTx(tx, owner)
		if err != nil {
			t.Fatal(err)
		}
		items, err := service.New(owned).FindPayments(context.Background(), model.Filter{})
		want := 0
		if owner == "123" {
			want = 4
		}
		if err != nil || len(items) != want {
			t.Fatalf("owner=%s count=%d err=%v", owner, len(items), err)
		}
	}
}
