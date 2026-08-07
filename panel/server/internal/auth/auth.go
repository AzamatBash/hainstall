package auth

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const CookieName = "hapanel_session"

type Service struct {
	password   string
	jwtSecret  []byte
	ttl        time.Duration
	limiter    *LoginLimiter
	cookiePath string
	secure     bool
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func New(password, secret string, ttl time.Duration) *Service {
	return NewWithCookie(password, secret, ttl, "/", false)
}

func NewWithCookie(password, secret string, ttl time.Duration, cookiePath string, secure bool) *Service {
	if cookiePath == "" {
		cookiePath = "/"
	}
	return &Service{
		password:   password,
		jwtSecret:  []byte(secret),
		ttl:        ttl,
		limiter:    NewLoginLimiter(),
		cookiePath: cookiePath,
		secure:     secure,
	}
}

// Limiter returns the per-IP login attempt tracker.
func (s *Service) Limiter() *LoginLimiter {
	return s.limiter
}

func (s *Service) CheckPassword(password string) bool {
	return password != "" && password == s.password
}

func (s *Service) IssueToken() (string, time.Time, error) {
	exp := time.Now().UTC().Add(s.ttl)
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "admin",
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
		},
	})
	signed, err := t.SignedString(s.jwtSecret)
	return signed, exp, err
}

func (s *Service) ParseToken(tokenStr string) error {
	tok, err := jwt.ParseWithClaims(tokenStr, &claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return err
	}
	c, ok := tok.Claims.(*claims)
	if !ok || !tok.Valid || c.Role != "admin" {
		return errors.New("invalid token")
	}
	return nil
}

func (s *Service) TokenFromRequest(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		return c.Value
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func (s *Service) SetSessionCookie(w http.ResponseWriter, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     s.cookiePath,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  exp,
	})
}

func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     s.cookiePath,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}
