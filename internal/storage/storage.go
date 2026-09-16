package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// User хранит основные данные учетной записи зарегистрированного клиента.
type User struct {
	ID           string
	Login        string
	PasswordHash string
}

// Order хранит отправленный заказ со статусом обработки и начислениями.
type Order struct {
	Number     string
	UserID     string
	Status     string
	Accrual    *float64
	UploadedAt time.Time
}

// Withdrawal хранит завершенный запрос снятия баланса.
type Withdrawal struct {
	Order       string
	Sum         float64
	ProcessedAt time.Time
}

// Store оборачивает соединение PostgreSQL и все методы доступа к данным.
type Store struct {
	DB *sql.DB
}

// New создает Store, связанный с подключением к базе данных.
func New(db *sql.DB) *Store { return &Store{DB: db} }

// Open создает и проверяет подключение к базе данных.
func Open(uri string) (*sql.DB, error) {
	db, err := sql.Open("pgx", uri)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Init создает схему, используемую сервисом, если она еще не существует.
func (s *Store) Init() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			login TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			id SERIAL PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			number TEXT UNIQUE NOT NULL,
			status TEXT NOT NULL DEFAULT 'NEW',
			accrual NUMERIC(18,2),
			uploaded_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS withdrawals (
			id SERIAL PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			order_number TEXT NOT NULL,
			sum NUMERIC(18,2) NOT NULL,
			processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
	}
	for _, query := range queries {
		if _, err := s.DB.Exec(query); err != nil {
			return err
		}
	}
	return nil
}

// CreateUser вставляет нового пользователя и возвращает его идентификатор базы данных.
func (s *Store) CreateUser(userID, login, passwordHash string) error {
	_, err := s.DB.Exec(`INSERT INTO users (id, login, password_hash) VALUES ($1, $2, $3)`, userID, login, passwordHash)
	return err
}

// UserByLogin загружает пользователя по логину.
func (s *Store) UserByLogin(login string) (*User, error) {
	var user User
	err := s.DB.QueryRow(`SELECT id, login, password_hash FROM users WHERE login = $1`, login).Scan(&user.ID, &user.Login, &user.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UserByID загружает пользователя по идентификатору.
func (s *Store) UserByID(userID string) (*User, error) {
	var user User
	err := s.DB.QueryRow(`SELECT id, login, password_hash FROM users WHERE id = $1`, userID).Scan(&user.ID, &user.Login, &user.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FindOrderByNumber возвращает пользователя и статус для номера заказа, если он присутствует.
func (s *Store) FindOrderByNumber(number string) (string, string, error) {
	var userID string
	var status string
	err := s.DB.QueryRow(`SELECT user_id, status FROM orders WHERE number = $1`, number).Scan(&userID, &status)
	return userID, status, err
}

// InsertOrder сохраняет недавно отправленный заказ для пользователя.
func (s *Store) InsertOrder(userID string, number string) error {
	_, err := s.DB.Exec(`INSERT INTO orders (user_id, number, status, accrual) VALUES ($1, $2, 'NEW', NULL)`, userID, number)
	return err
}

// OrdersByUser загружает все заказы пользователя, отсортированные от новых к старым.
func (s *Store) OrdersByUser(userID string) ([]Order, error) {
	rows, err := s.DB.Query(`SELECT number, user_id, status, accrual, uploaded_at FROM orders WHERE user_id = $1 ORDER BY uploaded_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Order, 0)
	for rows.Next() {
		var item Order
		var accrual sql.NullString
		if err := rows.Scan(&item.Number, &item.UserID, &item.Status, &accrual, &item.UploadedAt); err != nil {
			return nil, err
		}
		if accrual.Valid {
			value, err := strconv.ParseFloat(accrual.String, 64)
			if err != nil {
				return nil, err
			}
			item.Accrual = &value
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// Balance рассчитывает текущие и выведенные итоги для пользователя.
func (s *Store) Balance(userID string) (float64, float64, error) {
	var totalAccrual, totalWithdrawn sql.NullString
	err := s.DB.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN status = 'PROCESSED' AND accrual IS NOT NULL THEN accrual ELSE 0 END), 0),
			COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0)
		FROM orders WHERE user_id = $1`, userID).Scan(&totalAccrual, &totalWithdrawn)
	if err != nil {
		return 0, 0, err
	}
	current, err := parseNullableFloat(totalAccrual)
	if err != nil {
		return 0, 0, err
	}
	withdrawn, err := parseNullableFloat(totalWithdrawn)
	if err != nil {
		return 0, 0, err
	}
	return current - withdrawn, withdrawn, nil
}

// InsertWithdrawal сохраняет операцию снятия.
func (s *Store) InsertWithdrawal(userID string, orderNumber string, sum float64) error {
	_, err := s.DB.Exec(`INSERT INTO withdrawals (user_id, order_number, sum, processed_at) VALUES ($1, $2, $3, now())`, userID, orderNumber, sum)
	return err
}

// WithdrawalsByUser загружает успешно обработанные снятия для пользователя, отсортированные новые в первую очередь.
func (s *Store) WithdrawalsByUser(userID string) ([]Withdrawal, error) {
	rows, err := s.DB.Query(`SELECT order_number, sum, processed_at FROM withdrawals WHERE user_id = $1 ORDER BY processed_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Withdrawal, 0)
	for rows.Next() {
		var item Withdrawal
		if err := rows.Scan(&item.Order, &item.Sum, &item.ProcessedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// PendingOrders возвращает заказы, требующие проверки обновления статуса.
func (s *Store) PendingOrders() ([]string, error) {
	rows, err := s.DB.Query(`SELECT number FROM orders WHERE status IN ('NEW', 'PROCESSING') ORDER BY uploaded_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]string, 0)
	for rows.Next() {
		var number string
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		orders = append(orders, number)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return orders, nil
}

// UpdateOrderStatus обновляет сохраненный статус обработки и данные начисления.
func (s *Store) UpdateOrderStatus(orderNumber, status string, accrual *float64) error {
	if accrual == nil {
		_, err := s.DB.Exec(`UPDATE orders SET status = $1, accrual = NULL WHERE number = $2`, status, orderNumber)
		return err
	}
	_, err := s.DB.Exec(`UPDATE orders SET status = $1, accrual = $2 WHERE number = $3`, status, *accrual, orderNumber)
	return err
}

func parseNullableFloat(value sql.NullString) (float64, error) {
	if !value.Valid || value.String == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(value.String, 64)
	if err != nil {
		return 0, fmt.Errorf("parse numeric value %q: %w", value.String, err)
	}
	return parsed, nil
}

// ErrNoRows возвращается, когда поиск по логину или id не нашел запись.
var ErrNoRows = errors.New("no rows found")
