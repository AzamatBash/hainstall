package main

import (
	"bytes"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/api"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/store"
)

func newMigratedDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func runCLI(args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = runAPIKeyCommand(args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestAPIKeyCLI_CreateAndList(t *testing.T) {
	path := newMigratedDB(t)

	code, out, errOut := runCLI("create", "--db", path, "--name", "vpn-expert", "--scope", "config")
	if code != 0 {
		t.Fatalf("create code=%d stderr=%s", code, errOut)
	}
	if !regexp.MustCompile(`^hp_[0-9a-f]{64}\n$`).MatchString(out) {
		t.Fatalf("stdout must be exactly the key line, got %q", out)
	}
	key := strings.TrimSpace(out)
	if strings.Contains(errOut, key) || strings.Contains(errOut, store.HashAPIKey(key)) {
		t.Fatal("stderr must not contain the key or its hash")
	}
	if !strings.Contains(errOut, "prefix="+key[:11]) || !strings.Contains(errOut, "scopes=config") || !strings.Contains(errOut, "id=") {
		t.Fatalf("stderr=%q", errOut)
	}

	code, out, errOut = runCLI("create", "--db", path, "--name", "collector")
	if code != 0 {
		t.Fatalf("legacy create code=%d stderr=%s", code, errOut)
	}
	legacyKey := strings.TrimSpace(out)
	if !strings.Contains(errOut, "scopes=-") {
		t.Fatalf("legacy stderr=%q", errOut)
	}

	// The keys work against the API of a panel using the same database.
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := api.New(st, auth.New("admin-pass", "jwt-secret-for-tests", time.Hour), agent.New(true),
		slog.New(slog.NewTextHandler(io.Discard, nil)), api.Options{})
	call := func(path, k string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-API-Key", k)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if c := call("/api/v1/config", key); c != http.StatusOK {
		t.Fatalf("config with CLI key=%d", c)
	}
	if c := call("/api/v1/hanodes", key); c != http.StatusOK {
		t.Fatalf("hanodes with CLI key=%d", c)
	}
	if c := call("/api/v1/config", legacyKey); c != http.StatusForbidden {
		t.Fatalf("config with legacy CLI key=%d", c)
	}
	if c := call("/api/v1/hanodes", legacyKey); c != http.StatusOK {
		t.Fatalf("hanodes with legacy CLI key=%d", c)
	}

	code, out, errOut = runCLI("list", "--db", path)
	if code != 0 {
		t.Fatalf("list code=%d stderr=%s", code, errOut)
	}
	for _, secret := range []string{key, legacyKey, store.HashAPIKey(key), store.HashAPIKey(legacyKey)} {
		if strings.Contains(out, secret) || strings.Contains(errOut, secret) {
			t.Fatal("list must never print keys or hashes")
		}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("list output:\n%s", out)
	}
	var expertLine string
	for _, l := range lines {
		if strings.Contains(l, "vpn-expert") {
			expertLine = l
		}
	}
	if !strings.Contains(expertLine, key[:11]) || !strings.Contains(expertLine, "config") || strings.Count(expertLine, "-") < 1 {
		t.Fatalf("expert line=%q", expertLine)
	}
	if !regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`).MatchString(expertLine) {
		t.Fatalf("expert line lacks timestamps: %q", expertLine)
	}
}

func TestAPIKeyCLI_DBPathFromEnv(t *testing.T) {
	path := newMigratedDB(t)
	t.Setenv("PANEL_DB_PATH", path)
	code, out, errOut := runCLI("create", "--scope", "config")
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, ok := st.LookupAPIKey(strings.TrimSpace(out))
	if !ok || m.Name != "export" || !m.HasScope(store.APIKeyScopeConfig) {
		t.Fatalf("ok=%v meta=%+v", ok, m)
	}
}

func TestAPIKeyCLI_Errors(t *testing.T) {
	path := newMigratedDB(t)
	if code, out, _ := runCLI("create", "--db", path, "--scope", "admin"); code != 2 || out != "" {
		t.Fatalf("unknown scope code=%d out=%q", code, out)
	}
	if code, _, _ := runCLI(); code != 2 {
		t.Fatalf("no subcommand code=%d", code)
	}
	if code, _, _ := runCLI("rotate"); code != 2 {
		t.Fatalf("unknown subcommand code=%d", code)
	}
	if code, _, _ := runCLI("list", "--db", path, "extra"); code != 2 {
		t.Fatalf("extra args code=%d", code)
	}

	missing := filepath.Join(t.TempDir(), "missing.db")
	if code, out, _ := runCLI("create", "--db", missing, "--scope", "config"); code != 1 || out != "" {
		t.Fatalf("missing db code=%d out=%q", code, out)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("CLI must not create a database")
	}

	// "file:" URIs and "?" parameters would bypass mode=rw and busy_timeout.
	for _, p := range []string{"file:" + missing, "file:" + missing + "?mode=rwc", "file:" + path, path + "?_pragma=busy_timeout(1)"} {
		if code, out, errOut := runCLI("create", "--db", p, "--scope", "config"); code != 1 || out != "" || !strings.Contains(errOut, "не поддерживается") {
			t.Fatalf("db %q code=%d out=%q stderr=%q", p, code, out, errOut)
		}
	}
	t.Setenv("PANEL_DB_PATH", "file:"+missing)
	if code, out, errOut := runCLI("list"); code != 1 || out != "" || !strings.Contains(errOut, "не поддерживается") {
		t.Fatalf("env file: URI code=%d out=%q stderr=%q", code, out, errOut)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("CLI must not create a database from a file: URI")
	}

	// A database the new panel has not started on yet (0.1.75 schema).
	old := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE api_keys (id TEXT PRIMARY KEY, name TEXT NOT NULL, prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	code, out, errOut := runCLI("create", "--db", old, "--scope", "config")
	if code != 1 || out != "" || !strings.Contains(errOut, "api_keys.scopes") {
		t.Fatalf("old schema code=%d out=%q stderr=%q", code, out, errOut)
	}
	if code, _, errOut := runCLI("list", "--db", old); code != 1 || !strings.Contains(errOut, "api_keys.scopes") {
		t.Fatalf("old schema list code=%d stderr=%q", code, errOut)
	}
}
