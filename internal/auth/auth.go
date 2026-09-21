package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

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

// signUserID создает подпись для userID
func (a *Authenticator) signUserID(userID string) string {
	mac := hmac.New(sha256.New, a.secretKey)
	mac.Write([]byte(userID))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifySignature проверяет подпись userID
func (a *Authenticator) verifySignature(userID, signature string) bool {
	expected := a.signUserID(userID)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// SetUserCookie устанавливает подписанную куку с userID
func (a *Authenticator) SetUserCookie(w http.ResponseWriter, userID string) {
	signature := a.signUserID(userID)
	cookieValue := base64.URLEncoding.EncodeToString([]byte(userID + ":" + signature))

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

// GetUserIDFromCookie извлекает и проверяет userID из куки
func (a *Authenticator) GetUserIDFromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", ErrNoCookie
		}
		return "", fmt.Errorf("failed to get cookie: %w", err)
	}

	// Декодируем значение
	decoded, err := base64.URLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return "", ErrInvalidCookie
	}

	// Разделяем userID и подпись
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", ErrInvalidCookie
	}

	userID := parts[0]
	signature := parts[1]

	// Проверяем подпись
	if !a.verifySignature(userID, signature) {
		return "", ErrInvalidCookie
	}

	return userID, nil
}

// UserIDFromRequest возвращает userID из куки
func (a *Authenticator) UserIDFromRequest(r *http.Request) (string, error) {
	return a.GetUserIDFromCookie(r)
}
