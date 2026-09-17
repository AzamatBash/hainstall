package haproxy

import (
	"runtime"
	"strings"
	"testing"

	"github.com/azabash/hapanel/agent/internal/store"
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
	body := BaseConfigBody([]store.Entrance{{Port: 8443, Backend: "app"}}, DefaultProtectOpts())
	for _, needle := range []string{
		"nbthread 2",
		"hard-stop-after 5m",
		"option  splice-auto",
		"frontend entrance_8443",
		"bind *:8443",
		"default_backend app",
		"tcp-request inspect-delay 5s",
		"req_ssl_hello_type 1",
		"stick-table type ip",
		"sc_conn_cur(0) gt 100",
		"sc_conn_rate(0) gt 60",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in:\n%s", needle, body)
		}
	}
}

func TestBaseConfigBodyMultiEntrance(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "1")
	body := BaseConfigBody([]store.Entrance{
		{Port: 443, Backend: "edge"},
		{Port: 8443, Backend: "app"},
	}, DefaultProtectOpts())
	for _, needle := range []string{
		"frontend entrance_443",
		"bind *:443",
		"default_backend edge",
		"frontend entrance_8443",
		"bind *:8443",
		"default_backend app",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in:\n%s", needle, body)
		}
	}
	if strings.Contains(body, "frontend https_front") {
		t.Fatalf("legacy single frontend should be gone:\n%s", body)
	}
}

func TestBaseConfigBodyProtectOff(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "1")
	body := BaseConfigBody([]store.Entrance{{Port: 8443, Backend: "app"}}, ProtectOpts{})
	if strings.Contains(body, "inspect-delay") || strings.Contains(body, "stick-table") {
		t.Fatalf("expected no protect rules:\n%s", body)
	}
}
