package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenExisting_RefusesURIAndQueryPaths(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "panel.db")
	st, err := Open(existing)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.db")

	for _, p := range []string{
		"file:" + missing,
		"file:" + missing + "?mode=rwc",
		" file:" + missing,
		"file:" + existing,
		"file:" + existing + "?mode=rw&_pragma=busy_timeout(5000)",
		existing + "?_pragma=busy_timeout(1)",
		missing + "?mode=rwc",
		"file::memory:?cache=shared",
		missing + "#x",
		filepath.Join(dir, "missing#.db"),
	} {
		s, err := OpenExisting(p)
		if err == nil {
			_ = s.Close()
			t.Fatalf("OpenExisting(%q) must be refused", p)
		}
		if !strings.Contains(err.Error(), "не поддерживается") || !strings.Contains(err.Error(), "file:") {
			t.Fatalf("OpenExisting(%q) err=%v", p, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "missing") {
			t.Fatalf("OpenExisting created %s", e.Name())
		}
	}
}

func readPragmas(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"busy_timeout", "synchronous", "foreign_keys", "journal_mode"} {
		var v string
		if err := db.QueryRow(`PRAGMA ` + name).Scan(&v); err != nil {
			t.Fatalf("pragma %s: %v", name, err)
		}
		out[name] = v
	}
	return out
}

func TestOpenExisting_SamePragmasAsOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	srv, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	want := readPragmas(t, srv.db)
	got := readPragmas(t, cli.db)
	if want["busy_timeout"] != "5000" || want["synchronous"] != "1" || want["foreign_keys"] != "1" || want["journal_mode"] != "wal" {
		t.Fatalf("server pragmas changed: %v", want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("OpenExisting pragma %s=%q, Open has %q (all: %v vs %v)", k, got[k], v, got, want)
		}
	}
}

// A CLI write next to the live server waits for the server's write lock
// (busy_timeout) instead of failing with "database is locked".
func TestOpenExisting_WaitsForLiveWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	srv, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cli, err := OpenExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	ctx := context.Background()
	conn, err := srv.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO providers (id, name, created_at) VALUES ('cli-lock-test', 'lock', '2026-10-26T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	released := make(chan error, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, err := conn.ExecContext(ctx, `COMMIT`)
		released <- err
	}()
	created, err := cli.CreateAPIKeyWithScopes("cli", []string{APIKeyScopeConfig})
	if err != nil {
		t.Fatalf("CLI write next to a live writer: %v", err)
	}
	if err := <-released; err != nil {
		t.Fatal(err)
	}
	if m, ok := srv.LookupAPIKey(created.Key); !ok || !m.HasScope(APIKeyScopeConfig) {
		t.Fatalf("server does not see the CLI key: ok=%v meta=%+v", ok, m)
	}
}

// OpenExisting leaves the file's journal mode alone (no persistent change),
// even on a database it then refuses.
func TestOpenExisting_KeepsJournalMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyAPIKeysDB(t, path, "hp_"+strings.Repeat("aa", 32))
	if _, err := OpenExisting(path); err == nil || !strings.Contains(err.Error(), "api_keys.scopes") {
		t.Fatalf("legacy schema err=%v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "delete" {
		t.Fatalf("journal_mode=%q, OpenExisting must not change it", mode)
	}
}
