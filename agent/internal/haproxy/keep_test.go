package haproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/azabash/hapanel/agent/internal/store"
)

func TestEnsureBaseConfigLeavesTunedFile(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "2")
	dir := t.TempDir()
	prev := `# tuned by hand
global
    maxconn 200000
    nbthread 4
    hard-stop-after 30m
defaults
    mode tcp
    timeout connect 10s
    timeout client 1h
    timeout server 1h
    retries 3
frontend entrance_8443
    mode tcp
    bind *:8443
    maxconn 100000
    tcp-request inspect-delay 10s
    acl is_tls_clienthello req_ssl_hello_type 1
    tcp-request content accept if is_tls_clienthello
    tcp-request content reject
    default_backend app
`
	path := filepath.Join(dir, BaseConfigFile)
	if err := os.WriteFile(path, []byte(prev), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureBaseConfig(context.Background(), dir, nil, nil, []store.Entrance{{Port: 8443, Backend: "app"}}, DefaultProtectOpts())
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("startup must not rewrite a config that already binds the entrance")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != prev {
		t.Fatalf("file changed:\n%s", got)
	}
}

func TestBaseConfigBodyKeepingCopiesTuning(t *testing.T) {
	t.Setenv("HAPROXY_NBTHREAD", "1")
	prev := `global
    maxconn 200000
    nbthread 4
defaults
    timeout connect 10s
    timeout client 1h
    timeout server 1h
    retries 3
frontend entrance_8443
    bind *:8443
    maxconn 100000
    tcp-request inspect-delay 10s
    tcp-request content reject
    default_backend app
`
	body := BaseConfigBodyKeeping(prev, []store.Entrance{
		{Port: 8443, Backend: "app"},
		{Port: 443, Backend: "edge"},
	}, ProtectOpts{}, false)
	for _, needle := range []string{
		"maxconn 200000",
		"nbthread 4",
		"timeout client 1h",
		"retries 3",
		"maxconn 100000",
		"tcp-request inspect-delay 10s",
		"frontend entrance_443",
		"default_backend edge",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("missing %q in:\n%s", needle, body)
		}
	}
	if strings.Contains(body, "timeout client  30m") || strings.Contains(body, "retries 2") {
		t.Fatalf("template defaults leaked:\n%s", body)
	}
}

func TestWriteLeavesWatcherFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	w := NewConfigWriter(dir)
	servers := []store.Server{{
		Name: "foo", Backend: "app", Address: "10.0.0.1", Port: 443, Weight: 100,
	}}
	if err := w.Write(servers, map[string]string{"app": "leastconn"}); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(dir, "app.cfg")
	raw, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	tuned := strings.Replace(string(raw), "    balance leastconn\n", "    balance leastconn\n    timeout server 1h\n", 1)
	tuned = strings.Replace(tuned, "weight 100\n", "weight 100 check inter 2000 rise 2 fall 3\n", 1)
	if err := os.WriteFile(appPath, []byte(tuned), 0o644); err != nil {
		t.Fatal(err)
	}
	appBefore, err := os.Stat(appPath)
	if err != nil {
		t.Fatal(err)
	}
	managedBefore, err := os.Stat(filepath.Join(dir, "managed.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := w.Write(servers, map[string]string{"app": "leastconn"}); err != nil {
		t.Fatal(err)
	}
	appAfter, err := os.Stat(appPath)
	if err != nil {
		t.Fatal(err)
	}
	managedAfter, err := os.Stat(filepath.Join(dir, "managed.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if !appBefore.ModTime().Equal(appAfter.ModTime()) {
		t.Fatal("app.cfg mtime changed")
	}
	if !managedBefore.ModTime().Equal(managedAfter.ModTime()) {
		t.Fatal("managed.cfg mtime changed")
	}
	got, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != tuned {
		t.Fatalf("watcher lines dropped:\n%s", got)
	}
}

func TestMergeKeepsCheckBeforeWeight(t *testing.T) {
	prev := `# Managed by hapanel agent — do not edit by hand
backend app
    mode tcp
    balance leastconn
    option tcp-check
    server fr1 10.0.0.1:443 check inter 3s fall 3 rise 2 ssl verify none weight 100
    server fr2 10.0.0.2:8443 weight 50 check port 443 inter 2s
`
	gen, err := renderBackendSection("app", []store.Server{
		{Name: "fr2", Address: "10.0.0.2", Port: 8443, Weight: 50},
		{Name: "fr1", Address: "10.0.0.1", Port: 443, Weight: 100},
	}, "leastconn")
	if err != nil {
		t.Fatal(err)
	}
	if got := mergeBackendDirectives(prev, gen, false); got != prev {
		t.Fatalf("watcher file rewritten:\n%s", got)
	}
	changed, err := renderBackendSection("app", []store.Server{
		{Name: "fr1", Address: "10.0.0.1", Port: 443, Weight: 40},
		{Name: "fr2", Address: "10.0.0.2", Port: 8443, Weight: 50},
	}, "leastconn")
	if err != nil {
		t.Fatal(err)
	}
	got := mergeBackendDirectives(prev, changed, false)
	if got == prev || !strings.Contains(got, "server fr1 10.0.0.1:443 weight 40\n") {
		t.Fatalf("weight change was not applied:\n%s", got)
	}
}

func TestStartupKeepsWatcherWeight(t *testing.T) {
	dir := t.TempDir()
	w := NewConfigWriter(dir)
	prev := `# Managed by hapanel agent — do not edit by hand
backend app
    mode tcp
    balance leastconn
    server fr1 10.0.0.1:443 check inter 3s fall 3 rise 2 ssl verify none weight 25
    server plain 10.0.0.9:443 weight 100
`
	if err := os.WriteFile(filepath.Join(dir, "app.cfg"), []byte(prev), 0o644); err != nil {
		t.Fatal(err)
	}
	servers := []store.Server{
		{Name: "fr1", Backend: "app", Address: "10.0.0.1", Port: 443, Weight: 100},
		{Name: "plain", Backend: "app", Address: "10.0.0.9", Port: 443, Weight: 100},
	}
	before, err := os.Stat(filepath.Join(dir, "app.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := w.WriteStartup(servers, map[string]string{"app": "leastconn"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(dir, "app.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("startup rewrote app.cfg despite matching addresses")
	}
	got, err := os.ReadFile(filepath.Join(dir, "app.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != prev {
		t.Fatalf("startup changed watcher weight:\n%s", got)
	}

	servers[0].Weight = 25
	servers[0].Address = "10.0.0.2"
	if err := w.WriteStartup(servers, map[string]string{"app": "leastconn"}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "app.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "server fr1 10.0.0.2:443 weight 25\n") {
		t.Fatalf("address change was not applied:\n%s", got)
	}
	if !strings.Contains(string(got), "server plain 10.0.0.9:443 weight 100\n") {
		t.Fatalf("unchanged server was rewritten:\n%s", got)
	}
}

func TestMergeBackendKeepsServerMaxconn(t *testing.T) {
	prev := `# Managed by hapanel agent — do not edit by hand
backend app
    mode tcp
    balance leastconn
    timeout server 1h
    retries 3
    server foo 10.0.0.1:443 weight 100 maxconn 3000
`
	gen, err := renderBackendSection("app", []store.Server{{
		Name: "foo", Address: "10.0.0.1", Port: 443, Weight: 100,
	}}, "leastconn")
	if err != nil {
		t.Fatal(err)
	}
	got := mergeBackendDirectives(prev, gen, false)
	if !strings.Contains(got, "timeout server 1h") || !strings.Contains(got, "retries 3") {
		t.Fatalf("backend directives dropped:\n%s", got)
	}
	if !strings.Contains(got, "server foo 10.0.0.1:443 weight 100 maxconn 3000") {
		t.Fatalf("server maxconn dropped:\n%s", got)
	}
}
