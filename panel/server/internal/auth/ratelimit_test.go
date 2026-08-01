package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestLoginLimiterLocksAfterFiveFails(t *testing.T) {
	l := NewLoginLimiter()
	ip := "203.0.113.10"

	for i := 1; i <= MaxLoginAttempts-1; i++ {
		locked, _ := l.RecordFailure(ip)
		if locked {
			t.Fatalf("attempt %d should not lock yet", i)
		}
		if locked, _ := l.Check(ip); locked {
			t.Fatalf("check after attempt %d should not be locked", i)
		}
	}

	locked, msg := l.RecordFailure(ip)
	if !locked {
		t.Fatal("5th failure should lock")
	}
	if msg == "" {
		t.Fatal("expected Russian lockout message")
	}

	locked, _ = l.Check(ip)
	if !locked {
		t.Fatal("expected lockout on Check")
	}

	// Successful login clears.
	l.Clear(ip)
	if locked, _ := l.Check(ip); locked {
		t.Fatal("expected clear after success")
	}
}

func TestLoginLimiterExpires(t *testing.T) {
	l := NewLoginLimiter()
	ip := "203.0.113.11"
	l.mu.Lock()
	l.byIP[ip] = &loginAttempt{lockedUntil: time.Now().Add(-time.Second)}
	l.mu.Unlock()

	locked, _ := l.Check(ip)
	if locked {
		t.Fatal("expired lockout should clear")
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "192.0.2.1:12345"
	if got := ClientIP(r); got != "192.0.2.1" {
		t.Fatalf("got %q", got)
	}
	r.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.1")
	if got := ClientIP(r); got != "198.51.100.1" {
		t.Fatalf("xff got %q", got)
	}
}
