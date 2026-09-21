package accrual

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryAfter(t *testing.T) {
	if got := retryAfter("5"); got != 5*time.Second {
		t.Fatalf("retryAfter(5) = %s, want 5s", got)
	}
	if got := retryAfter("invalid"); got != defaultRetryAfter {
		t.Fatalf("retryAfter(invalid) = %s, want %s", got, defaultRetryAfter)
	}
}

func TestFetchOrderRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := New(server.URL).FetchOrder("123")
	var rateLimitErr *RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("FetchOrder() error = %v, want RateLimitError", err)
	}
	if rateLimitErr.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %s, want 7s", rateLimitErr.RetryAfter)
	}
}
