package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestCookieRoundTrip(t *testing.T) {
	authenticator := New("secret")
	response := httptest.NewRecorder()
	authenticator.SetUserCookie(response, "user-1")
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(response.Result().Cookies()[0])

	got, err := authenticator.UserIDFromRequest(request)
	if err != nil || got != "user-1" {
		t.Fatalf("UserIDFromRequest() = %q, %v", got, err)
	}
}

func TestExpiredCookieRejected(t *testing.T) {
	const secret = "secret"
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if _, err := New(secret).GetUserIDFromCookie(request); !errors.Is(err, ErrInvalidCookie) {
		t.Fatalf("expired cookie error = %v, want %v", err, ErrInvalidCookie)
	}
}

func TestJWTRejectsWrongSecret(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString([]byte("other-secret"))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if _, err := New("secret").GetUserIDFromCookie(request); !errors.Is(err, ErrInvalidCookie) {
		t.Fatalf("wrong secret error = %v, want %v", err, ErrInvalidCookie)
	}
}

func TestJWTRejectsWrongSigningMethod(t *testing.T) {
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	if _, err := New("secret").GetUserIDFromCookie(request); !errors.Is(err, ErrInvalidCookie) {
		t.Fatalf("wrong algorithm error = %v, want %v", err, ErrInvalidCookie)
	}
}

func TestJWTRejectsMissingSubjectOrExpiry(t *testing.T) {
	tests := []struct {
		name  string
		claim jwt.Claims
	}{
		{name: "missing subject", claim: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
		{name: "missing expiry", claim: jwt.RegisteredClaims{Subject: "user-1"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, test.claim).SignedString([]byte("secret"))
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.AddCookie(&http.Cookie{Name: cookieName, Value: token})
			if _, err := New("secret").GetUserIDFromCookie(request); !errors.Is(err, ErrInvalidCookie) {
				t.Fatalf("claim validation error = %v, want %v", err, ErrInvalidCookie)
			}
		})
	}
}

func TestCookieErrors(t *testing.T) {
	authenticator := New("secret")
	tests := []struct {
		name   string
		cookie string
		want   error
	}{
		{name: "missing", want: ErrNoCookie},
		{name: "invalid base64", cookie: "%%%", want: ErrInvalidCookie},
		{name: "missing separator", cookie: "dXNlcg==", want: ErrInvalidCookie},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			if test.cookie != "" {
				request.AddCookie(&http.Cookie{Name: cookieName, Value: test.cookie})
			}
			_, err := authenticator.GetUserIDFromCookie(request)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTamperedCookieAndDefaultSecret(t *testing.T) {
	response := httptest.NewRecorder()
	New("").SetUserCookie(response, "user-1")
	cookie := response.Result().Cookies()[0]
	cookie.Value = cookie.Value[:len(cookie.Value)-1] + "A"
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookie)
	if _, err := New("").GetUserIDFromCookie(request); !errors.Is(err, ErrInvalidCookie) {
		t.Fatalf("tampered cookie error = %v, want %v", err, ErrInvalidCookie)
	}
}
