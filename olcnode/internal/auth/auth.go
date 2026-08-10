package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// Bearer validates Authorization: Bearer <token> against the configured token.
type Bearer struct {
	Token string
}

// Middleware rejects requests without a valid bearer token.
// Empty configured token yields 500 (misconfiguration).
func (b Bearer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.Token == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"server misconfigured: empty token"}`))
			return
		}
		got := extractBearer(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare([]byte(got), []byte(b.Token)) != 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func extractBearer(h string) string {
	const prefix = "Bearer "
	if len(h) < len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}
