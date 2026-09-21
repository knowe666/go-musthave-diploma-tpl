package accrual

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client взаимодействует с сервисом начисления баллов.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// OrderInfo описывает результат сервиса начисления баллов для одного заказа.
type OrderInfo struct {
	Order   string   `json:"order"`
	Status  string   `json:"status"`
	Accrual *float64 `json:"accrual,omitempty"`
}

// RateLimitError сообщает, сколько нужно подождать перед следующим запросом.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("accrual request rate limited: retry after %s", e.RetryAfter)
}

const maxRetries = 3
const defaultRetryAfter = 60 * time.Second

// New создает клиент начисления баллов, настроенный с базовым URL.
func New(baseURL string) *Client {
	if !strings.Contains(baseURL, "://") && baseURL != "" {
		baseURL = "http://" + baseURL
	}
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// FetchOrder получает текущие данные начисления баллов для конкретного заказа.
func (c *Client) FetchOrder(orderNumber string) (*OrderInfo, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("accrual service is not configured")
	}
	url := c.BaseURL + "/api/orders/" + orderNumber
	for retry := 0; retry <= maxRetries; retry++ {
		resp, err := c.HTTPClient.Get(url)
		if err != nil {
			return nil, err
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &RateLimitError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
		}
		if resp.StatusCode == http.StatusOK {
			var info OrderInfo
			if err := json.Unmarshal(body, &info); err != nil {
				return nil, err
			}
			return &info, nil
		}

		if resp.StatusCode >= http.StatusInternalServerError && retry < maxRetries {
			time.Sleep(time.Second << retry)
			continue
		}
		return nil, fmt.Errorf("accrual request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil, fmt.Errorf("accrual request failed after retries")
}

func retryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err == nil {
		if seconds < 0 {
			logRetryAfterError(value, fmt.Errorf("negative delay"))
			return defaultRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}

	when, err := http.ParseTime(value)
	if err != nil {
		logRetryAfterError(value, err)
		return defaultRetryAfter
	}
	if delay := time.Until(when); delay > 0 {
		return delay
	}
	return 0
}

func logRetryAfterError(value string, err error) {
	log.Printf("accrual: invalid Retry-After %q: %v; using default delay %s", value, err, defaultRetryAfter)
}
