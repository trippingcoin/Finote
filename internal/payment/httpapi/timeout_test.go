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
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	_ "github.com/lib/pq"
)

func TestHardTimeoutReturnsJSONAndCancelsHandler(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			done := make(chan struct{})
			release := make(chan struct{})
			h := withTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
				close(done)
				<-release // prove the response is bounded even before handler return
			}), time.Second, 30*time.Millisecond)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, "/v1/payments", nil))
			close(release)
			if w.Code != 503 || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("response=%d %v", w.Code, w.Header())
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["code"] != "request_timeout" {
				t.Fatalf("body=%s err=%v", w.Body, err)
			}
			if method != "GET" && body["error"] != unknownOutcome {
				t.Fatal("mutation outcome must be unknown")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("handler not canceled")
			}
		})
	}
}

func TestOperationDeadlineAndCancellation(t *testing.T) {
	done := make(chan struct{})
	h := withTimeout(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		respondError(w, r, fmt.Errorf("database canceled: %w", r.Context().Err()))
		close(done)
	}), 30*time.Millisecond, time.Second)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/v1/users", nil))
	if w.Code != 503 {
		t.Fatalf("status=%d", w.Code)
	}
	<-done
}

func TestDatabaseLockTimeoutDoesNotCreateExpense(t *testing.T) {
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated FINOTE_TEST_DATABASE_URL required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("timeout_%d", time.Now().UnixNano())
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
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("LOCK TABLE payments IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(service.New(repo.New(db))))
	defer server.Close()
	client := server.Client()
	client.Timeout = 9 * time.Second
	started := time.Now()
	res, err := client.Post(server.URL+"/v1/payments", "application/json", strings.NewReader(`{"user_id":"123","amount":400,"category":"еда"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body map[string]string
	if err = json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 503 || body["code"] != "request_timeout" || body["error"] != unknownOutcome {
		t.Fatalf("status=%d body=%v", res.StatusCode, body)
	}
	if time.Since(started) > 7*time.Second {
		t.Fatal("SQL wait exceeded operation budget")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow("SELECT count(*) FROM payments").Scan(&count); err != nil || count != 0 {
		t.Fatalf("late insert: %d %v", count, err)
	}
}
