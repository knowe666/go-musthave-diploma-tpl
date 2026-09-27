package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/auth"
	"github.com/knowe666/go-musthave-diploma-tpl/internal/storage"
)

type mockStore struct {
	user            *storage.User
	userByLoginErr  error
	userByIDErr     error
	createUserErr   error
	orders          []storage.Order
	ordersErr       error
	balance         [2]float64
	balanceErr      error
	withdrawErr     error
	withdrawals     []storage.Withdrawal
	withdrawalsErr  error
	findOrderUserID string
	findOrderErr    error
	insertOrderErr  error
	findOrderStatus string
	createdUser     *storage.User
	withdrawAmount  float64
}

func (m *mockStore) UserByLogin(context.Context, string) (*storage.User, error) {
	return m.user, m.userByLoginErr
}
func (m *mockStore) UserByID(context.Context, string) (*storage.User, error) {
	return m.user, m.userByIDErr
}
func (m *mockStore) CreateUser(_ context.Context, id, login, passwordHash string) error {
	m.createdUser = &storage.User{ID: id, Login: login, PasswordHash: passwordHash}
	return m.createUserErr
}
func (m *mockStore) FindOrderByNumber(context.Context, string) (string, string, error) {
	return m.findOrderUserID, m.findOrderStatus, m.findOrderErr
}
func (m *mockStore) InsertOrder(context.Context, string, string) error { return m.insertOrderErr }
func (m *mockStore) OrdersByUser(context.Context, string) ([]storage.Order, error) {
	return m.orders, m.ordersErr
}
func (m *mockStore) Balance(context.Context, string) (float64, float64, error) {
	return m.balance[0], m.balance[1], m.balanceErr
}
func (m *mockStore) Withdraw(_ context.Context, _, _ string, amount float64) error {
	m.withdrawAmount = amount
	return m.withdrawErr
}
func (m *mockStore) WithdrawalsByUser(context.Context, string) ([]storage.Withdrawal, error) {
	return m.withdrawals, m.withdrawalsErr
}
func (m *mockStore) PendingOrders(context.Context) ([]string, error)                   { return nil, nil }
func (m *mockStore) UpdateOrderStatus(context.Context, string, string, *float64) error { return nil }

func newTestService(store *mockStore) *Service { return New(store, auth.New("test-secret"), nil) }

func requestWithCookie(t *testing.T, method, target, body, userID string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	auth.New("test-secret").SetUserCookie(recorder, userID)
	request.AddCookie(recorder.Result().Cookies()[0])
	return request
}

func TestIsDigits(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "digits", value: "123456", want: true},
		{name: "empty", value: "", want: false},
		{name: "letters", value: "123a", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isDigits(test.value); got != test.want {
				t.Fatalf("isDigits(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}

func TestLuhnValid(t *testing.T) {
	if !luhnValid("79927398713") {
		t.Fatal("expected valid Luhn number")
	}
	if luhnValid("79927398714") {
		t.Fatal("expected invalid Luhn number")
	}
}

func TestNormalizeAccrualStatus(t *testing.T) {
	tests := map[string]string{
		"REGISTERED": "NEW",
		"PROCESSING": "PROCESSING",
		"INVALID":    "INVALID",
		"PROCESSED":  "PROCESSED",
		"UNKNOWN":    "NEW",
	}

	for input, want := range tests {
		if got := normalizeAccrualStatus(input); got != want {
			t.Errorf("normalizeAccrualStatus(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRegisterAndLogin(t *testing.T) {
	store := &mockStore{userByLoginErr: sql.ErrNoRows}
	service := newTestService(store)

	register := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{"login":"alice","password":"secret"}`))
	registerResponse := httptest.NewRecorder()
	service.Router().ServeHTTP(registerResponse, register)
	if registerResponse.Code != http.StatusOK || store.createdUser == nil {
		t.Fatalf("register status = %d, user = %#v", registerResponse.Code, store.createdUser)
	}
	if store.createdUser.PasswordHash == "secret" {
		t.Fatal("password must be stored as a hash")
	}

	store.user = store.createdUser
	store.userByLoginErr = nil
	login := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"login":"alice","password":"secret"}`))
	loginResponse := httptest.NewRecorder()
	service.Router().ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK || len(loginResponse.Result().Cookies()) != 1 {
		t.Fatalf("login status = %d, cookies = %d", loginResponse.Code, len(loginResponse.Result().Cookies()))
	}
}

func TestRegisterAndLoginErrors(t *testing.T) {
	service := newTestService(&mockStore{userByLoginErr: sql.ErrNoRows})
	for _, body := range []string{"not-json", `{"login":"","password":"secret"}`} {
		response := httptest.NewRecorder()
		service.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("register invalid status = %d, want 400", response.Code)
		}
	}

	store := &mockStore{userByLoginErr: sql.ErrNoRows}
	service = newTestService(store)
	response := httptest.NewRecorder()
	service.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"login":"alice","password":"secret"}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown login status = %d, want 401", response.Code)
	}

	store.userByLoginErr = errors.New("db")
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{"login":"alice","password":"secret"}`)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("login storage error status = %d, want 500", response.Code)
	}
}

func TestOrderListBalanceAndWithdrawals(t *testing.T) {
	userID := "user-1"
	store := &mockStore{
		user:        &storage.User{ID: userID},
		orders:      []storage.Order{{Number: "79927398713", Status: "PROCESSED", Accrual: float64Ptr(12.5), UploadedAt: time.Unix(0, 0).UTC()}},
		balance:     [2]float64{10, 2.5},
		withdrawals: []storage.Withdrawal{{Order: "79927398713", Sum: 2.5, ProcessedAt: time.Unix(0, 0).UTC()}},
	}
	service := newTestService(store)

	for _, test := range []struct{ name, path string }{
		{name: "orders", path: "/api/user/orders"},
		{name: "balance", path: "/api/user/balance"},
		{name: "withdrawals", path: "/api/user/withdrawals"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, test.path, "", userID))
			if response.Code != http.StatusOK || response.Body.Len() == 0 {
				t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestWithdraw(t *testing.T) {
	store := &mockStore{user: &storage.User{ID: "user-1"}}
	service := newTestService(store)
	response := httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodPost, "/api/user/balance/withdraw", `{"order":"79927398713","sum":2.5}`, "user-1"))
	if response.Code != http.StatusOK || store.withdrawAmount != 2.5 {
		t.Fatalf("status = %d, amount = %v", response.Code, store.withdrawAmount)
	}

	store.withdrawErr = storage.ErrInsufficientFunds
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodPost, "/api/user/balance/withdraw", `{"order":"79927398713","sum":2.5}`, "user-1"))
	if response.Code != http.StatusPaymentRequired {
		t.Fatalf("insufficient funds status = %d, want 402", response.Code)
	}
}

func TestUnauthorizedRequest(t *testing.T) {
	service := newTestService(&mockStore{})
	response := httptest.NewRecorder()
	service.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/user/balance", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestInvalidJWTCookieRejected(t *testing.T) {
	service := newTestService(&mockStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	response := httptest.NewRecorder()
	auth.New("other-secret").SetUserCookie(response, "user-1")
	request.AddCookie(response.Result().Cookies()[0])

	service.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("bad jwt status = %d, want 401", response.Code)
	}
}

func TestExpiredJWTCookieRejected(t *testing.T) {
	service := newTestService(&mockStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	response := httptest.NewRecorder()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: "user_id", Value: token})

	service.Router().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired jwt status = %d, want 401", response.Code)
	}
}

func TestOrderSubmit(t *testing.T) {
	userID := "user-1"
	newRequest := func(number string) *http.Request {
		return requestWithCookie(t, http.MethodPost, "/api/user/orders", number, userID)
	}

	store := &mockStore{user: &storage.User{ID: userID}, findOrderErr: sql.ErrNoRows}
	service := newTestService(store)
	response := httptest.NewRecorder()
	service.Router().ServeHTTP(response, newRequest("79927398713"))
	if response.Code != http.StatusAccepted {
		t.Fatalf("new order status = %d, want 202", response.Code)
	}

	store.findOrderErr = nil
	store.findOrderUserID = userID
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, newRequest("79927398713"))
	if response.Code != http.StatusOK {
		t.Fatalf("same order status = %d, want 200", response.Code)
	}

	store.findOrderUserID = "other-user"
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, newRequest("79927398713"))
	if response.Code != http.StatusConflict {
		t.Fatalf(" чужой order status = %d, want 409", response.Code)
	}

	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, newRequest("123"))
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid order status = %d, want 422", response.Code)
	}
}

func TestEmptyListsAndStorageErrors(t *testing.T) {
	userID := "user-1"
	for _, test := range []struct {
		name string
		path string
		set  func(*mockStore)
		want int
	}{
		{name: "empty orders", path: "/api/user/orders", set: func(s *mockStore) { s.orders = nil }, want: http.StatusNoContent},
		{name: "empty withdrawals", path: "/api/user/withdrawals", set: func(s *mockStore) { s.withdrawals = nil }, want: http.StatusNoContent},
		{name: "balance error", path: "/api/user/balance", set: func(s *mockStore) { s.balanceErr = errors.New("db") }, want: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &mockStore{user: &storage.User{ID: userID}}
			test.set(store)
			response := httptest.NewRecorder()
			newTestService(store).Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, test.path, "", userID))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestProtectedHandlerStorageErrors(t *testing.T) {
	userID := "user-1"
	store := &mockStore{user: &storage.User{ID: userID}, userByIDErr: errors.New("db")}
	service := newTestService(store)
	response := httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, "/api/user/orders", "", userID))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("orders user lookup status = %d, want 401", response.Code)
	}

	store.userByIDErr = nil
	store.ordersErr = errors.New("db")
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, "/api/user/orders", "", userID))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("orders storage error status = %d, want 500", response.Code)
	}
	store.ordersErr = nil
	store.withdrawalsErr = errors.New("db")
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, "/api/user/withdrawals", "", userID))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("withdrawals storage error status = %d, want 500", response.Code)
	}
	store.withdrawalsErr = nil
	store.balanceErr = errors.New("db")
	response = httptest.NewRecorder()
	service.Router().ServeHTTP(response, requestWithCookie(t, http.MethodGet, "/api/user/balance", "", userID))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("balance storage error status = %d, want 500", response.Code)
	}
}

func float64Ptr(value float64) *float64 { return &value }
