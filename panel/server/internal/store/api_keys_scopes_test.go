package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAPIKeyScopes(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "keys.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	legacy, err := st.CreateAPIKey("collector")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacy.Scopes, []string{}) {
		t.Fatalf("legacy scopes=%#v", legacy.Scopes)
	}
	expert, err := st.CreateAPIKeyWithScopes("expert", []string{" config", "config", ""})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expert.Scopes, []string{"config"}) {
		t.Fatalf("expert scopes=%#v", expert.Scopes)
	}
	if _, err := st.CreateAPIKeyWithScopes("bad", []string{"admin"}); err == nil || !strings.Contains(err.Error(), "неизвестное право") {
		t.Fatalf("unknown scope err=%v", err)
	}
	if _, err := st.CreateAPIKeyWithScopes("bad", []string{"Config"}); err == nil {
		t.Fatal("scope tokens are case-sensitive")
	}

	m, ok := st.LookupAPIKey(expert.Key)
	if !ok || m.ID != expert.ID || !m.HasScope(APIKeyScopeConfig) || m.LastUsedAt != nil {
		t.Fatalf("lookup expert ok=%v meta=%+v", ok, m)
	}
	m, ok = st.LookupAPIKey(legacy.Key)
	if !ok || m.HasScope(APIKeyScopeConfig) || !reflect.DeepEqual(m.Scopes, []string{}) {
		t.Fatalf("lookup legacy ok=%v meta=%+v", ok, m)
	}
	if !st.ValidAPIKey(legacy.Key) || !st.ValidAPIKey(expert.Key) {
		t.Fatal("both keys must stay valid for hanodes")
	}
	if _, ok := st.LookupAPIKey("hp_deadbeef"); ok {
		t.Fatal("random key must not resolve")
	}

	list, err := st.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]APIKeyMeta{}
	for _, k := range list {
		byID[k.ID] = k
	}
	if got := byID[expert.ID]; !reflect.DeepEqual(got.Scopes, []string{"config"}) || got.LastUsedAt == nil {
		t.Fatalf("listed expert=%+v", got)
	}
	if got := byID[legacy.ID]; !reflect.DeepEqual(got.Scopes, []string{}) {
		t.Fatalf("listed legacy=%+v", got)
	}

	if ok, err := st.RevokeAPIKey(expert.ID); err != nil || !ok {
		t.Fatalf("revoke ok=%v err=%v", ok, err)
	}
	if _, ok := st.LookupAPIKey(expert.Key); ok {
		t.Fatal("revoked key must not resolve")
	}
}

func TestMigrateTwiceKeepsKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateAPIKeyWithScopes("expert", []string{"config"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer st.Close()
	m, ok := st.LookupAPIKey(created.Key)
	if !ok || !m.HasScope(APIKeyScopeConfig) {
		t.Fatalf("after reopen ok=%v meta=%+v", ok, m)
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('api_keys') WHERE name = 'scopes'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("scopes columns=%d", n)
	}
}

// createLegacyAPIKeysDB writes the 0.1.75 api_keys table (no scopes) and one key.
func createLegacyAPIKeysDB(t *testing.T, path, rawKey string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at TEXT
);
INSERT INTO api_keys (id, name, prefix, key_hash, created_at)
VALUES ('legacy-1', 'old', 'hp_00000000', '` + HashAPIKey(rawKey) + `', '2026-10-16T12:00:00Z');`); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateAddsScopesToLegacyKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	raw := "hp_" + strings.Repeat("ab", 32)
	createLegacyAPIKeysDB(t, path, raw)

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m, ok := st.LookupAPIKey(raw)
	if !ok || m.ID != "legacy-1" || !reflect.DeepEqual(m.Scopes, []string{}) {
		t.Fatalf("legacy key after migrate ok=%v meta=%+v", ok, m)
	}
	if !st.ValidAPIKey(raw) {
		t.Fatal("legacy key must keep hanodes access")
	}
}

func TestOpenExisting(t *testing.T) {
	dir := t.TempDir()

	missing := filepath.Join(dir, "missing.db")
	if _, err := OpenExisting(missing); err == nil {
		t.Fatal("missing database must fail")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("OpenExisting must not create the file, stat err=%v", err)
	}

	legacy := filepath.Join(dir, "legacy.db")
	createLegacyAPIKeysDB(t, legacy, "hp_"+strings.Repeat("cd", 32))
	if _, err := OpenExisting(legacy); err == nil || !strings.Contains(err.Error(), "api_keys.scopes") {
		t.Fatalf("legacy schema err=%v", err)
	}

	// Has api_keys.scopes but nothing else: OpenExisting must not migrate.
	partial := filepath.Join(dir, "partial.db")
	createLegacyAPIKeysDB(t, partial, "hp_"+strings.Repeat("ef", 32))
	db, err := sql.Open("sqlite", partial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE api_keys ADD COLUMN scopes TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	st, err := OpenExisting(partial)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	created, err := st.CreateAPIKeyWithScopes("cli", []string{"config"})
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := st.LookupAPIKey(created.Key); !ok || !m.HasScope(APIKeyScopeConfig) {
		t.Fatalf("created via OpenExisting ok=%v meta=%+v", ok, m)
	}
	var tables int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'nodes'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("OpenExisting must not run migrations")
	}
}
