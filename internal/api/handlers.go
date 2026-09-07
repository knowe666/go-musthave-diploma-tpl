package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/knowe666/internal/accrual"
	"github.com/knowe666/internal/auth"
	"github.com/knowe666/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

// Service является точкой входа HTTP API для сервиса gophermart.
type Service struct {
	Store   *storage.Store
	Auth    *auth.Authenticator
	Accrual *accrual.Client
}

// New создает экземпляр сервиса, который связывает зависимости storage, auth и accrual.
func New(store *storage.Store, authn *auth.Authenticator, client *accrual.Client) *Service {
	return &Service{Store: store, Auth: authn, Accrual: client}
}

// Router создает маршруты API для сервиса gophermart.
func (s *Service) Router() http.Handler {
	r := chi.NewRouter()

	r.Post("/api/user/register", s.handleRegister)
	r.Post("/api/user/login", s.handleLogin)

	r.Post("/api/user/orders", s.handleOrderSubmitChi)
	r.Get("/api/user/orders", s.handleOrderListChi)

	r.Get("/api/user/balance", s.handleBalance)
	r.Post("/api/user/balance/withdraw", s.handleWithdraw)

	r.Get("/api/user/withdrawals", s.handleWithdrawals)

	return r
}

// SyncPendingOrders опрашивает сервис начисления баллов на предмет заказов, которые все еще ожидают обработки.
func (s *Service) SyncPendingOrders() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		orders, err := s.Store.PendingOrders()
		if err != nil {
			log.Printf("query pending orders: %v", err)
			continue
		}
		for _, number := range orders {
			if err := s.syncOrderStatus(number); err != nil {
				log.Printf("sync order %s: %v", number, err)
			}
		}
	}
}

func (s *Service) handleOrderSubmitChi(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := s.Store.UserByID(userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.handleOrderSubmit(w, r, user)
}

func (s *Service) handleOrderListChi(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := s.Store.UserByID(userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.handleOrderList(w, user)
}

func (s *Service) handleRegister(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.Login) == "" || strings.TrimSpace(request.Password) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if _, err := s.Store.UserByLogin(request.Login); err == nil {
		http.Error(w, "login already exists", http.StatusConflict)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("check user: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("hash password: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	userID := auth.GenerateUserID()
	if err := s.Store.CreateUser(userID, request.Login, string(hash)); err != nil {
		log.Printf("create user: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	s.Auth.SetUserCookie(w, userID)
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(request.Login) == "" || strings.TrimSpace(request.Password) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	user, err := s.Store.UserByLogin(request.Login)
	if errors.Is(err, sql.ErrNoRows) || err != nil && user == nil {
		http.Error(w, "invalid login or password", http.StatusUnauthorized)
		return
	}
	if err != nil {
		log.Printf("query user: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)); err != nil {
		http.Error(w, "invalid login or password", http.StatusUnauthorized)
		return
	}

	s.Auth.SetUserCookie(w, user.ID)
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handleOrders(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	user, err := s.Store.UserByID(userID)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		s.handleOrderSubmit(w, r, user)
	case http.MethodGet:
		s.handleOrderList(w, user)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Service) handleOrderSubmit(w http.ResponseWriter, r *http.Request, user *storage.User) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	orderNumber := strings.TrimSpace(string(body))
	if orderNumber == "" || !isDigits(orderNumber) || !luhnValid(orderNumber) {
		http.Error(w, "invalid order format", http.StatusUnprocessableEntity)
		return
	}

	foundUserID, _, err := s.Store.FindOrderByNumber(orderNumber)
	if err == nil {
		if foundUserID == user.ID {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "order already uploaded by another user", http.StatusConflict)
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		log.Printf("find order: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if err := s.Store.InsertOrder(user.ID, orderNumber); err != nil {
		log.Printf("insert order: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := s.syncOrderStatus(orderNumber); err != nil {
		log.Printf("sync accrual status for %s: %v", orderNumber, err)
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) handleOrderList(w http.ResponseWriter, user *storage.User) {
	orders, err := s.Store.OrdersByUser(user.ID)
	if err != nil {
		log.Printf("load orders: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if len(orders) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	response := make([]map[string]interface{}, 0, len(orders))
	for _, order := range orders {
		item := map[string]interface{}{
			"number":      order.Number,
			"status":      order.Status,
			"uploaded_at": order.UploadedAt.Format(time.RFC3339),
		}
		if order.Accrual != nil {
			item["accrual"] = *order.Accrual
		}
		response = append(response, item)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("encode order list: %v", err)
	}
}

func (s *Service) handleBalance(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	current, withdrawn, err := s.Store.Balance(userID)
	if err != nil {
		log.Printf("query balance: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]float64{"current": current, "withdrawn": withdrawn}); err != nil {
		log.Printf("encode balance: %v", err)
	}
}

func (s *Service) handleWithdraw(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var request struct {
		Order string  `json:"order"`
		Sum   float64 `json:"sum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if request.Order == "" || !isDigits(request.Order) || request.Sum <= 0 {
		http.Error(w, "invalid order or sum", http.StatusUnprocessableEntity)
		return
	}

	current, _, err := s.Store.Balance(userID)
	if err != nil {
		log.Printf("query balance: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if current < request.Sum {
		http.Error(w, "insufficient funds", http.StatusPaymentRequired)
		return
	}
	if err := s.Store.InsertWithdrawal(userID, request.Order, request.Sum); err != nil {
		log.Printf("insert withdrawal: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) handleWithdrawals(w http.ResponseWriter, r *http.Request) {
	userID, err := s.Auth.UserIDFromRequest(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := s.Store.WithdrawalsByUser(userID)
	if err != nil {
		log.Printf("load withdrawals: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if len(items) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	response := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		response = append(response, map[string]interface{}{
			"order":        item.Order,
			"sum":          item.Sum,
			"processed_at": item.ProcessedAt.Format(time.RFC3339),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("encode withdrawals: %v", err)
	}
}

func (s *Service) syncOrderStatus(orderNumber string) error {
	if s.Accrual == nil {
		return nil
	}
	info, err := s.Accrual.FetchOrder(orderNumber)
	if err != nil {
		return err
	}
	status := normalizeAccrualStatus(info.Status)
	if status == "PROCESSED" && info.Accrual == nil {
		status = "INVALID"
	}
	return s.Store.UpdateOrderStatus(orderNumber, status, info.Accrual)
}

func normalizeAccrualStatus(status string) string {
	switch status {
	case "REGISTERED":
		return "NEW"
	case "PROCESSING":
		return "PROCESSING"
	case "INVALID":
		return "INVALID"
	case "PROCESSED":
		return "PROCESSED"
	default:
		return "NEW"
	}
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func luhnValid(number string) bool {
	if !isDigits(number) {
		return false
	}
	digits := make([]int, len(number))
	for i, ch := range number {
		digits[i] = int(ch - '0')
	}
	sum := 0
	parity := len(digits) % 2
	for i, digit := range digits {
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}
	return sum%10 == 0
}
