package haproxy

import (
	"runtime"
	"strings"
	"testing"
)

func TestNbthreadCountDefault(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "")
	got := nbthreadCount()
	want := runtime.NumCPU()
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Fatalf("nbthreadCount()=%d want %d", got, want)
	}
}

func TestNbthreadCountEnv(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "2")
	if got := nbthreadCount(); got != 2 {
		t.Fatalf("env override: got %d", got)
	}
}

func TestBaseConfigBodyOptimizations(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "2")
	body := BaseConfigBody()
	for _, needle := range []string{
		"nbthread 2",
		"hard-stop-after 5m",
		"option  splice-auto",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in:\n%s", needle, body)
		}
	}
}
