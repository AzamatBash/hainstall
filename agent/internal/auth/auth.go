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

// Middleware returns an HTTP middleware that rejects requests without a valid bearer token.
// Health checks may be exempted by the caller; this middleware protects everything it wraps.
func (b Bearer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.Token == "" {
			http.Error(w, `{"error":"server misconfigured: empty token"}`, http.StatusInternalServerError)
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
