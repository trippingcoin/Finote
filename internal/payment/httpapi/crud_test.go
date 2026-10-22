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
)

func apiFixture(t *testing.T) *httptest.Server {
	t.Helper()
	dsn := os.Getenv("FINOTE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated FINOTE_TEST_DATABASE_URL required")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("crud_%d", time.Now().UnixNano())
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
	if err = migrations.Apply(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(New(service.New(repo.New(db))))
	t.Cleanup(server.Close)
	return server
}

func apiRequest(t *testing.T, s *httptest.Server, method, path, body string, status int, target any) {
	t.Helper()
	req, err := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d", method, path, res.StatusCode, status)
	}
	if status != 204 && res.Header.Get("Content-Type") != "application/json" {
		t.Fatal(res.Header)
	}
	if target != nil {
		if err = json.NewDecoder(res.Body).Decode(target); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPaymentCRUDAndValidation(t *testing.T) {
	s := apiFixture(t)
	var p model.Payment
	apiRequest(t, s, "POST", "/v1/payments", `{"user_id":"10","amount":400,"category":"еда","comment":"кофе","spent_at":"2026-10-21T10:00:00+05:00"}`, 201, &p)
	if p.ID == "" || p.Version != 1 || p.CreatedAt.IsZero() {
		t.Fatal(p)
	}
	path := "/v1/payments/" + p.ID
	apiRequest(t, s, "GET", path, "", 200, &p)
	// comment is omitted when empty; decode into a fresh value, like a real client.
	p = model.Payment{}
	apiRequest(t, s, "PATCH", path, `{"comment":""}`, 200, &p)
	if p.Comment != "" || p.Amount != 400 || p.Category != "еда" || p.Version != 2 {
		t.Fatal(p)
	}
	apiRequest(t, s, "PUT", path, `{"amount":700,"category":"транспорт","comment":"такси","spent_at":"2026-10-22T10:00:00+05:00"}`, 200, &p)
	if p.Amount != 700 || p.Category != "транспорт" || p.Version != 3 {
		t.Fatal(p)
	}
	for _, body := range []string{`{}`, `{"amount":0}`, `{"amount":-1}`, `{"amount":1.001}`, `{"category":"unknown"}`, `{"spent_at":"bad"}`, `{"user_id":"20"}`, `{"amount":500} {}`, `null`, `[]`, `{"amount":"400"}`} {
		apiRequest(t, s, "PATCH", path, body, 400, nil)
	}
	apiRequest(t, s, "PATCH", path, `{"comment":"`+strings.Repeat("я", 501)+`"}`, 400, nil)
	apiRequest(t, s, "PATCH", path, `{"comment":"`+strings.Repeat("x", maxRequestBodyBytes)+`"}`, 400, nil)
	apiRequest(t, s, "GET", path, "", 200, &p)
	if p.Amount != 700 || p.Version != 3 {
		t.Fatal("invalid input mutated row", p)
	}
	apiRequest(t, s, "GET", "/v1/payments/bad-id", "", 400, nil)
	apiRequest(t, s, "DELETE", path, "", 204, nil)
	for _, method := range []string{"GET", "DELETE"} {
		apiRequest(t, s, method, path, "", 404, nil)
	}
	apiRequest(t, s, "PATCH", path, `{"amount":500}`, 404, nil)
}

func TestPaymentFiltersTimezonePaginationAndTotals(t *testing.T) {
	s := apiFixture(t)
	for _, body := range []string{
		`{"user_id":"10","amount":100,"category":"еда","spent_at":"2026-10-20T19:00:00Z"}`,
		`{"user_id":"10","amount":200,"category":"еда","spent_at":"2026-10-21T18:59:59Z"}`,
		`{"user_id":"10","amount":400,"category":"еда","spent_at":"2026-10-21T19:00:00Z"}`,
		`{"user_id":"20","amount":800,"category":"транспорт","spent_at":"2026-10-21T10:00:00Z"}`,
	} {
		apiRequest(t, s, "POST", "/v1/payments", body, 201, nil)
	}
	for _, tc := range []struct {
		query          string
		total, members int64
		amount         string
		items          int
	}{
		{"?limit=1&offset=1", 4, 2, "1500.00", 1},
		{"?user_id=10&category=" + url.QueryEscape("еда") + "&from=2026-10-21&to=2026-10-21&timezone=Asia%2FAlmaty", 2, 1, "300.00", 2},
		{"?user_id=20", 1, 1, "800.00", 1}, {"?user_id=999", 0, 0, "0", 0},
		{"?offset=10", 4, 2, "1500.00", 0},
	} {
		var list model.PaymentList
		apiRequest(t, s, "GET", "/v1/payments"+tc.query, "", 200, &list)
		if list.Total != tc.total || list.Members != tc.members || list.Amount != tc.amount || len(list.Items) != tc.items {
			t.Fatalf("%s: %+v", tc.query, list)
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "offset=-1", "from=bad", "from=2026-10-22&to=2026-10-21", "timezone=bad", "category=bad"} {
		apiRequest(t, s, "GET", "/v1/payments?"+query, "", 400, nil)
	}
}
