package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"finote/internal/payment/httpapi"
	"finote/internal/payment/repo"
	"finote/internal/payment/service"
	_ "github.com/lib/pq"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set FINOTE_TEST_DATABASE_URL to an isolated test database")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("outbox_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
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
	t.Cleanup(func() { db.Close() })
	s := NewStore(db, time.UTC)
	if err = s.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func addExpense(t *testing.T, s *Store, update int64, user string) {
	t.Helper()
	a, c := 400.0, "еда"
	if _, err := s.Process(context.Background(), "123", update, user, Intent{Action: "create", Amount: &a, Category: &c}); err != nil {
		t.Fatal(err)
	}
}
func makeDue(t *testing.T, s *Store) {
	t.Helper()
	if _, err := s.db.Exec("UPDATE telegram_outbox SET available_at=CURRENT_TIMESTAMP WHERE status='pending'"); err != nil {
		t.Fatal(err)
	}
}
func assertCount(t *testing.T, s *Store, table string, want int) {
	t.Helper()
	var got int
	if err := s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil || got != want {
		t.Fatalf("%s count=%d want=%d err=%v", table, got, want, err)
	}
}
func assertStatus(t *testing.T, s *Store, id int64, want string) {
	t.Helper()
	var got string
	if err := s.db.QueryRow("SELECT status FROM telegram_outbox WHERE update_id=$1", id).Scan(&got); err != nil || got != want {
		t.Fatalf("status=%q want=%q err=%v", got, want, err)
	}
}
func TestOutboxAtomicDeduplication(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, c := 400.0, "еда"
			_, err := s.Process(context.Background(), "123", 1, "10", Intent{Action: "create", Amount: &a, Category: &c})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCount(t, s, "payments", 1)
	assertCount(t, s, "telegram_updates", 1)
	assertCount(t, s, "telegram_outbox", 1)
	// Failure to persist the reply must roll back the expense and receipt.
	if _, err := s.db.Exec("ALTER TABLE telegram_outbox ADD CONSTRAINT reject_two CHECK(update_id<>2)"); err != nil {
		t.Fatal(err)
	}
	a, c := 500.0, "еда"
	if _, err := s.Process(context.Background(), "123", 2, "10", Intent{Action: "create", Amount: &a, Category: &c}); err == nil {
		t.Fatal("expected outbox insert error")
	}
	assertCount(t, s, "payments", 1)
	assertCount(t, s, "telegram_updates", 1)
}
func TestDeliveryRecoveryAfterRestart(t *testing.T) {
	s := testStore(t)
	addExpense(t, s, 1, "10")
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			fmt.Fprint(w, "<html>unavailable</html>")
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer server.Close()
	b := &Bot{endpoint: server.URL + "/", id: "123", client: server.Client(), store: s}
	if _, err := b.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "pending")
	assertCount(t, s, "payments", 1)
	if _, found, err := s.NextReply(context.Background(), "123"); err != nil || found {
		t.Fatalf("backoff not honored: %v %v", found, err)
	}
	// Recreate both delivery objects, keeping only the durable database.
	fail.Store(false)
	makeDue(t, s)
	restarted := &Bot{endpoint: server.URL + "/", id: "123", client: server.Client(), store: NewStore(s.db, time.UTC)}
	if _, err := restarted.deliverNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "sent")
	assertCount(t, s, "payments", 1)
	addExpense(t, s, 1, "10")
	if _, found, err := s.NextReply(context.Background(), "123"); err != nil || found {
		t.Fatalf("delivered receipt requeued: %v %v", found, err)
	}
}
func TestLeaseAndChatOrdering(t *testing.T) {
	s := testStore(t)
	addExpense(t, s, 1, "10")
	addExpense(t, s, 2, "10")
	addExpense(t, s, 3, "20")
	first, ok, err := s.NextReply(context.Background(), "123")
	if err != nil || !ok || first.UpdateID != 1 {
		t.Fatal(first, ok, err)
	}
	other, ok, err := s.NextReply(context.Background(), "123")
	if err != nil || !ok || other.UpdateID != 3 {
		t.Fatal(other, ok, err)
	}
	if _, ok, err = s.NextReply(context.Background(), "123"); err != nil || ok {
		t.Fatal("lease or order ignored")
	}
	makeDue(t, s)
	recovered, ok, err := s.NextReply(context.Background(), "123")
	if err != nil || !ok || recovered.UpdateID != 1 {
		t.Fatal(recovered, ok, err)
	}
	if err = s.FinishReply(context.Background(), "123", first, "sent", 0, ""); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, s, 1, "pending") // stale worker cannot finish a new lease
	if err = s.FinishReply(context.Background(), "123", recovered, "sent", 0, ""); err != nil {
		t.Fatal(err)
	}
	next, ok, err := s.NextReply(context.Background(), "123")
	if err != nil || !ok || next.UpdateID != 2 {
		t.Fatal(next, ok, err)
	}
}
func TestPermanentDeliveryFailureAndRetryAfter(t *testing.T) {
	for _, code := range []int{403, 401, 429} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			s := testStore(t)
			addExpense(t, s, 1, "10")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(code)
				fmt.Fprintf(w, `{"ok":false,"error_code":%d,"parameters":{"retry_after":60}}`, code)
			}))
			defer server.Close()
			b := &Bot{endpoint: server.URL + "/", id: "123", client: server.Client(), store: s}
			_, err := b.deliverNext(context.Background())
			if critical(err) != (code == 401) {
				t.Fatalf("critical=%v code=%d", err, code)
			}
			want := "pending"
			if code == 403 {
				want = "failed"
			}
			assertStatus(t, s, 1, want)
			if code == 429 {
				var delay float64
				if err = s.db.QueryRow("SELECT EXTRACT(EPOCH FROM available_at-CURRENT_TIMESTAMP) FROM telegram_outbox").Scan(&delay); err != nil || delay < 58 {
					t.Fatalf("retry_after ignored: %f %v", delay, err)
				}
			}
		})
	}
}
func TestTelegramFailureDoesNotStopPanel(t *testing.T) {
	s := testStore(t)
	telegramServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"ok":false,"error_code":401}`)
	}))
	defer telegramServer.Close()
	b := &Bot{endpoint: telegramServer.URL + "/", id: "123", client: telegramServer.Client(), store: s}
	panel := httptest.NewServer(httpapi.New(service.New(repo.New(s.db))))
	defer panel.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); RunIsolated(ctx, b.Run) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("critical bot failure should stop bot")
	}
	if ctx.Err() != nil {
		t.Fatal("bot canceled application context")
	}
	for _, path := range []string{"/", "/v1/payments"} {
		res, err := panel.Client().Get(panel.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
}
func TestSupervisorRetriesTemporaryErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	RunIsolated(ctx, func(context.Context) error {
		calls++
		if calls == 2 {
			cancel()
			return nil
		}
		return errors.New("temporary database outage")
	})
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestPollingAcknowledgesDurableReplyDespiteDelivery503(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	delivered := make(chan struct{})
	var once sync.Once
	var acknowledged atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/getWebhookInfo":
			fmt.Fprint(w, `{"ok":true,"result":{"url":""}}`)
		case "/getUpdates":
			var req struct {
				Offset int64 `json:"offset"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if req.Offset == 0 {
				fmt.Fprint(w, `{"ok":true,"result":[{"update_id":1,"message":{"text":"/start","from":{"id":10},"chat":{"id":10,"type":"private"}}}]}`)
			} else {
				acknowledged.Store(req.Offset == 2)
				select {
				case <-delivered:
				case <-r.Context().Done():
				}
				cancel()
				fmt.Fprint(w, `{"ok":true,"result":[]}`)
			}
		case "/sendMessage":
			w.WriteHeader(503)
			fmt.Fprint(w, `{"ok":false,"error_code":503}`)
			once.Do(func() { close(delivered) })
		default:
			w.WriteHeader(501)
		}
	}))
	defer server.Close()
	b := &Bot{endpoint: server.URL + "/", id: "123", client: server.Client(), store: s, location: time.UTC}
	_ = b.Run(ctx)
	if !acknowledged.Load() {
		t.Fatal("polling blocked on delivery")
	}
	assertStatus(t, s, 1, "pending")
	assertCount(t, s, "telegram_updates", 1)
}
