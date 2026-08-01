package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	MaxLoginAttempts = 5
	LoginLockout     = 15 * time.Minute
)

type loginAttempt struct {
	fails       int
	lockedUntil time.Time
}

// LoginLimiter tracks failed password attempts per client IP (in-memory).
type LoginLimiter struct {
	mu   sync.Mutex
	byIP map[string]*loginAttempt
}

func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{byIP: make(map[string]*loginAttempt)}
}

// Check returns whether the IP is currently locked out and a Russian error message.
func (l *LoginLimiter) Check(ip string) (locked bool, msg string) {
	if ip == "" {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.byIP[ip]
	if a == nil {
		return false, ""
	}
	now := time.Now()
	if a.lockedUntil.After(now) {
		mins := int(a.lockedUntil.Sub(now).Minutes()) + 1
		if mins < 1 {
			mins = 1
		}
		return true, fmt.Sprintf("Слишком много попыток. Подождите %d мин.", mins)
	}
	// Lockout expired — reset counter.
	if !a.lockedUntil.IsZero() {
		delete(l.byIP, ip)
	}
	return false, ""
}

// RecordFailure increments the fail counter; after MaxLoginAttempts the IP is locked.
func (l *LoginLimiter) RecordFailure(ip string) (locked bool, msg string) {
	if ip == "" {
		return false, ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.byIP[ip]
	if a == nil {
		a = &loginAttempt{}
		l.byIP[ip] = a
	}
	now := time.Now()
	if a.lockedUntil.After(now) {
		mins := int(a.lockedUntil.Sub(now).Minutes()) + 1
		if mins < 1 {
			mins = 1
		}
		return true, fmt.Sprintf("Слишком много попыток. Подождите %d мин.", mins)
	}
	a.fails++
	if a.fails >= MaxLoginAttempts {
		a.lockedUntil = now.Add(LoginLockout)
		a.fails = 0
		mins := int(LoginLockout.Minutes())
		return true, fmt.Sprintf("Слишком много попыток. Подождите %d мин.", mins)
	}
	return false, ""
}

// Clear resets the fail counter for an IP (call on successful login).
func (l *LoginLimiter) Clear(ip string) {
	if ip == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
}

// ClientIP extracts the client address from the request.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
