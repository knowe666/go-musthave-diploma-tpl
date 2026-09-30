package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	cookieName   = "user_id"
	cookieMaxAge = 24 * 60 * 60 // 24 часа
)

var (
	ErrInvalidCookie = errors.New("invalid or expired cookie")
	ErrNoCookie      = errors.New("no cookie found")
)

// Authenticator описывает контракт для работы с cookie-аутентификацией.
type Authenticator struct {
	secretKey []byte
}

// New создаёт экземпляр аутентификатора с injected секретом.
func New(secret string) *Authenticator {
	if secret == "" {
		secret = "gophermart-auth-secret"
	}
	return &Authenticator{secretKey: []byte(secret)}
}

// GenerateUserID генерирует новый уникальный ID пользователя
func GenerateUserID() string {
	return uuid.New().String()
}

// SetUserCookie устанавливает подписанную куку с userID и сроком действия.
func (a *Authenticator) SetUserCookie(w http.ResponseWriter, userID string) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cookieMaxAge) * time.Second)),
	}
	cookieValue, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secretKey)
	if err != nil {
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    cookieValue,
		Path:     "/",
		MaxAge:   cookieMaxAge,
		HttpOnly: true,
		Secure:   false, // В продакшене должен быть true
		SameSite: http.SameSiteLaxMode,
	})
}

// GetUserIDFromCookie извлекает userID и проверяет подпись и срок действия токена.
func (a *Authenticator) GetUserIDFromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", ErrNoCookie
		}
		return "", ErrInvalidCookie
	}

	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(cookie.Value, claims, func(token *jwt.Token) (any, error) {
		return a.secretKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid || claims.Subject == "" {
		return "", ErrInvalidCookie
	}

	return claims.Subject, nil
}

// UserIDFromRequest возвращает userID из куки
func (a *Authenticator) UserIDFromRequest(r *http.Request) (string, error) {
	return a.GetUserIDFromCookie(r)
}
