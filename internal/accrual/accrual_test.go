package accrual

import (
	"context"
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

	_, err := New(server.URL).FetchOrder(context.Background(), "123")
	var rateLimitErr *RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("FetchOrder() error = %v, want RateLimitError", err)
	}
	if rateLimitErr.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %s, want 7s", rateLimitErr.RetryAfter)
	}
}

func TestFetchOrderCancellationDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		cancel()
	}))
	defer server.Close()

	_, err := New(server.URL).FetchOrder(ctx, "123")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("FetchOrder() error = %v, want context.Canceled", err)
	}
}
