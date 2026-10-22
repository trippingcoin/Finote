package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type pingFunc func(context.Context) error

func (f pingFunc) PingContext(c context.Context) error { return f(c) }

func TestHealthAndReadiness(t *testing.T) {
	for _, broken := range []bool{false, true} {
		db := pingFunc(func(ctx context.Context) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 2*time.Second {
				t.Error("missing bounded readiness timeout")
			}
			if broken {
				return errors.New("secret connection information")
			}
			return nil
		})
		h := healthHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) }), db)
		for _, path := range []string{"/healthz", "/readyz", "/"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			want := 200
			if path == "/" {
				want = 202
			}
			if broken && path == "/readyz" {
				want = 503
			}
			if w.Code != want {
				t.Fatalf("%s got %d want %d", path, w.Code, want)
			}
			if strings.Contains(w.Body.String(), "secret") {
				t.Fatal("leaked database error")
			}
		}
	}
}
