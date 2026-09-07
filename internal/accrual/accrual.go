package accrual

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("accrual request failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var info OrderInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}
