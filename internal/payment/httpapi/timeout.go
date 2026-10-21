package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const operationTimeout = 5 * time.Second
const responseTimeout = 8 * time.Second
const unknownOutcome = "Не удалось подтвердить результат операции. Проверьте список расходов перед повторной отправкой."

func timeoutResponse(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusServiceUnavailable, timeoutBody(r.Method))
}

func timeoutBody(method string) map[string]string {
	message := "Время ожидания истекло. Попробуйте обновить список."
	if method != http.MethodGet && method != http.MethodHead {
		message = unknownOutcome
	}
	return map[string]string{"error": message, "code": "request_timeout"}
}

// Context limits SQL (including pool/lock waits); TimeoutHandler bounds the
// response even when a handler is not promptly honoring context cancellation.
func withTimeout(next http.Handler, operation, hard time.Duration) http.Handler {
	bounded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), operation)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
	readBody, _ := json.Marshal(timeoutBody(http.MethodGet))
	writeBody, _ := json.Marshal(timeoutBody(http.MethodPost))
	read := http.TimeoutHandler(bounded, hard, string(readBody))
	write := http.TimeoutHandler(bounded, hard, string(writeBody))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			read.ServeHTTP(w, r)
		} else {
			write.ServeHTTP(w, r)
		}
	})
}
