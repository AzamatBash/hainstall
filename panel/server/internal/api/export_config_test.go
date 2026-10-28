package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/protect"
	"github.com/azabash/hapanel/panel/internal/remna"
	"github.com/azabash/hapanel/panel/internal/store"
)

var updateGolden = flag.Bool("update", false, "rewrite testdata golden files")

const (
	cfgNode1  = "6b0c1f3e-2a4d-4e5f-8a9b-0c1d2e3f4a51"
	cfgNode2  = "1d2e3f4a-5b6c-4d7e-8f9a-0b1c2d3e4f52"
	cfgNode3  = "9e8d7c6b-5a49-4382-a1b0-c9d8e7f6a553"
	cfgOrphan = "ffffffff-0000-4000-8000-0000000000ff"
	cfgPanelA = "a1b2c3d4-0000-4000-8000-00000000000a"
	cfgPanelB = "b1b2c3d4-0000-4000-8000-00000000000b"
	cfgProv1  = "5a000000-0000-4000-8000-000000000001"
	cfgProv2  = "4a000000-0000-4000-8000-000000000002"
	cfgAcc1   = "7c000000-0000-4000-8000-000000000001"
	cfgAcc2   = "2c000000-0000-4000-8000-000000000002"
	cfgExitA1 = "c0000000-0000-4000-8000-000000000001"
	cfgExitA2 = "c0000000-0000-4000-8000-000000000002"
	cfgExitA3 = "c0000000-0000-4000-8000-000000000003"
	cfgExitB1 = "d0000000-0000-4000-8000-000000000001"

	envExportKey = "test-env-export-key"
	jwtSecret    = "jwt-secret-for-tests"
)

// Strings seeded into secret or non-exported columns; none may reach /api/v1/config.
var configCanaries = []string{
	"CANARY-AGENT-TOKEN",
	"CANARY-REMNA-KEY",
	"canary-base-url",
	"canary-secret-path",
	"canary-login-url",
	"canary-favicon",
	"canary-provider-login",
	"canary-login-two",
	"CANARY-TASK",
	"CANARY-TEAM",
	"CANARY-OLC",
	"47893",       // management port inside nodes.url
	"/agent",      // management path inside nodes.url
	"https://203", // full management URL
	"0.1.13",      // agent_version lives in /api/v1/hanodes
}

type configFixture struct {
	path string
	st   *store.Store
	srv  *Server
	au   *auth.Service
}

func newConfigFixture(t *testing.T, agentURL, remnaURL string) *configFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	seedConfigFixture(t, path, agentURL, remnaURL)
	au := auth.New("admin-pass", jwtSecret, time.Hour)
	srv := New(st, au, agent.New(true), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		APIKey: envExportKey,
	})
	return &configFixture{path: path, st: st, srv: srv, au: au}
}

func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedConfigFixture writes deterministic rows (documentation IPs, example
// domains) so the golden example is stable. agentURL / remnaURL replace the
// node management URL and the Remnawave base URL of the first node / panel.
func seedConfigFixture(t *testing.T, path, agentURL, remnaURL string) {
	t.Helper()
	db := rawDB(t, path)
	snap1 := `{"sessions":7,"cpu":12.5,"load_avg":[0.1,0.2,0.3],"down_bps":1500000,"up_bps":200000,` +
		`"net_rx_bytes":1000,"net_tx_bytes":2000,"backends":[` +
		`{"backend":"app","name":"nl-exit-1","address":"198.51.100.21","port":443},` +
		`{"backend":"hap_agent","name":"agent","address":"127.0.0.1","port":9100},` +
		`{"backend":"app","name":"de-exit-2","address":"198.51.100.22","port":8443}],` +
		`"agent_version":"0.1.13","updated_at":"2026-10-26T11:59:55.123456Z"}`
	stmts := []struct {
		q    string
		args []any
	}{
		{`INSERT INTO nodes (id, name, url, token, created_at, last_seen, status, country, sort_order,
  snapshot, remna_panel_id, provider_id, provider_account_id, traffic_log, listen_ports, entrances, protect)
VALUES (?, 'fra-front-1', ?, 'CANARY-AGENT-TOKEN-7f3a', '2026-10-01T10:00:00.123456789Z',
  '2026-10-26T11:59:55Z', 'online', 'DE', 0, ?, ?, ?, ?, 1, '[443,8443]',
  '[{"port":443,"backend":"app"},{"port":8443,"backend":"reserve"}]', '')`,
			[]any{cfgNode1, agentURL, snap1, cfgPanelA, cfgProv1, cfgAcc1}},
		{`INSERT INTO nodes (id, name, url, token, created_at, status, country, sort_order,
  snapshot, remna_panel_id, provider_id, provider_account_id, traffic_log, listen_ports, entrances, protect)
VALUES (?, 'msk-front-2', 'http://[2001:db8::10]:47893/agent', 'CANARY-AGENT-TOKEN-91c2',
  '2026-10-02T08:30:00Z', 'offline', 'RU', 1, '', ?, '', '', 0, '[8443]', '',
  '{"client_hello":false,"client_hello_delay_sec":3,"rate_limit":true,"max_conn_per_ip":50,"max_rate_per_ip":0,"rate_window_sec":20,"ru_only":true,"synproxy":true}')`,
			[]any{cfgNode2, cfgPanelB}},
		{`INSERT INTO nodes (id, name, url, token, created_at, status, country, sort_order, snapshot)
VALUES (?, 'ams-front-3', 'not a url', '', '2026-10-03T09:15:00Z', 'unknown', 'NL', 2,
  '{"cpu":1,"updated_at":"2026-10-26T11:59:50Z"}')`,
			[]any{cfgNode3}},

		{`INSERT INTO remna_panels (id, name, base_url, api_key_enc, created_at) VALUES (?, 'Panel A', ?, ?, '2026-09-01T00:00:00Z')`,
			[]any{cfgPanelA, remnaURL, []byte("CANARY-REMNA-KEY-ciphertext")}},
		{`INSERT INTO remna_panels (id, name, base_url, api_key_enc, created_at) VALUES (?, 'Panel B', 'https://canary-base-url.example/b', NULL, '2026-09-02T00:00:00.5Z')`,
			[]any{cfgPanelB}},

		{`INSERT INTO remna_node_catalog (panel_id, remna_uuid, name, address, config_profile_uuid,
  inbound_uuids_json, inbound_tags, protocol_derived, protocol_override,
  role_rn_front, role_rn_back, role_hp_front, role_hp_back, role_cdn_back,
  enabled_in_analytics, notes, users_online, node_ok, last_seen_at, updated_at, alert_note)
VALUES (?, ?, 'nl-exit-1', 'nl-exit-1.example.net', 'e0000000-0000-4000-8000-00000000000e',
  '["f0000000-0000-4000-8000-000000000001","f0000000-0000-4000-8000-000000000002"]',
  'VLESS_REALITY, VLESS_XHTTP', 'vless_reality', '', 0, 1, 0, 1, 0, 1,
  'Exit behind <HP> front & CDN', 42, 1, 1761480000000, 1761480000000, 'Провайдер урезал канал')`,
			[]any{cfgPanelA, cfgExitA1}},
		{`INSERT INTO remna_node_catalog (panel_id, remna_uuid, name, address, inbound_uuids_json, inbound_tags,
  protocol_derived, protocol_override, users_online, node_ok, last_seen_at, updated_at)
VALUES (?, ?, 'de-exit-2a', '198.51.100.22', '[]', 'HY2', 'hysteria2', 'mixed', 9, 1, 1761480000000, 1761480000000)`,
			[]any{cfgPanelA, cfgExitA2}},
		{`INSERT INTO remna_node_catalog (panel_id, remna_uuid, name, address, inbound_uuids_json, inbound_tags,
  protocol_derived, role_cdn_back, users_online, last_seen_at, updated_at)
VALUES (?, ?, 'de-exit-2b', ' 198.51.100.22', 'not json', '', '', 1, 3, 1761480000000, 1761480000000)`,
			[]any{cfgPanelA, cfgExitA3}},
		{`INSERT INTO remna_node_catalog (panel_id, remna_uuid, name, address, inbound_uuids_json,
  protocol_derived, role_rn_front, role_hp_front, enabled_in_analytics, users_online, last_seen_at, updated_at)
VALUES (?, ?, 'ru-exit-1', 'nl-exit-1.example.net', '[]', 'unknown', 1, 1, 0, 5, 1761480000000, 1761480000000)`,
			[]any{cfgPanelB, cfgExitB1}},

		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'nl-exit-1', ?, ' NL-Exit-1.example.net ')`,
			[]any{cfgNode1, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'de-exit-2', ?, '198.51.100.22')`,
			[]any{cfgNode1, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'reserve', 'old-exit', ?, '192.0.2.99')`,
			[]any{cfgNode1, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'ru-exit', ?, 'nl-exit-1.example.net')`,
			[]any{cfgNode2, cfgPanelB}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'gone', ?, '192.0.2.50')`,
			[]any{cfgOrphan, cfgPanelA}},

		{`INSERT INTO providers (id, name, favicon_url, login_url, created_at) VALUES (?, 'Example Hosting', 'https://canary-favicon.example/f.ico', 'https://canary-login-url.example/login', '2026-08-01T00:00:00Z')`,
			[]any{cfgProv1}},
		{`INSERT INTO providers (id, name, created_at) VALUES (?, 'Other Cloud', '2026-08-02T00:00:00Z')`,
			[]any{cfgProv2}},
		{`INSERT INTO provider_accounts (id, provider_id, login, created_at) VALUES (?, ?, 'canary-provider-login@example.com', '2026-08-03T00:00:00Z')`,
			[]any{cfgAcc1, cfgProv1}},
		{`INSERT INTO provider_accounts (id, provider_id, login, created_at) VALUES (?, ?, 'canary-login-two', '2026-08-04T00:00:00Z')`,
			[]any{cfgAcc2, cfgProv2}},

		{`INSERT INTO tasks (id, title, description, created_at, updated_at) VALUES ('t1', 'CANARY-TASK-TITLE', 'CANARY-TASK-TEXT', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`, nil},
		{`INSERT INTO team_members (id, name, created_at) VALUES ('m1', 'CANARY-TEAM-MEMBER', '2026-10-01T00:00:00Z')`, nil},
		{`INSERT INTO olcrtc_nodes (id, name, agent_url, token, created_at, updated_at) VALUES ('o1', 'CANARY-OLC-NODE', 'https://canary-base-url.example/olc', 'CANARY-OLC-TOKEN', 1, 1)`, nil},
		{`INSERT INTO olcrtc_instances (id, node_id, name, provider, transport, room_id, key_hex, created_at, updated_at) VALUES ('i1', 'o1', 'CANARY-OLC-INSTANCE', 'p', 't', 'CANARY-OLC-ROOM', 'CANARY-OLC-KEYHEX', 1, 1)`, nil},
	}
	for i, s := range stmts {
		if _, err := db.Exec(s.q, s.args...); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
}

func (f *configFixture) get(t *testing.T, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (f *configFixture) jwt(t *testing.T) string {
	t.Helper()
	tok, _, err := f.au.IssueToken()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type configResponse struct {
	Schema       string                     `json:"schema"`
	Version      string                     `json:"version"`
	GeneratedAt  string                     `json:"generated_at"`
	SnapshotHash string                     `json:"snapshot_hash"`
	Sections     map[string]json.RawMessage `json:"sections"`
}

type sectionResponse struct {
	Status      string           `json:"status"`
	ContentHash *string          `json:"content_hash"`
	Items       []map[string]any `json:"items"`
}

func decodeConfig(t *testing.T, rec *httptest.ResponseRecorder) (configResponse, map[string]sectionResponse) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp configResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	secs := map[string]sectionResponse{}
	for name, raw := range resp.Sections {
		var sec sectionResponse
		if err := json.Unmarshal(raw, &sec); err != nil {
			t.Fatalf("section %s: %v", name, err)
		}
		secs[name] = sec
	}
	return resp, secs
}

func TestExportConfig_Scopes(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	legacy, err := f.st.CreateAPIKey("legacy")
	if err != nil {
		t.Fatal(err)
	}
	expert, err := f.st.CreateAPIKeyWithScopes("expert", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := f.st.CreateAPIKeyWithScopes("revoked", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.st.RevokeAPIKey(revoked.ID); err != nil || !ok {
		t.Fatalf("revoke ok=%v err=%v", ok, err)
	}
	jwt := f.jwt(t)

	cases := []struct {
		name    string
		hdr     map[string]string
		cookie  string
		hanodes int
		config  int
		errText string
	}{
		{name: "legacy key", hdr: map[string]string{"X-API-Key": legacy.Key}, hanodes: 200, config: 403, errText: "у ключа нет права config"},
		{name: "legacy key bearer", hdr: map[string]string{"Authorization": "Bearer " + legacy.Key}, hanodes: 200, config: 403, errText: "у ключа нет права config"},
		{name: "config key", hdr: map[string]string{"X-API-Key": expert.Key}, hanodes: 200, config: 200},
		{name: "config key bearer", hdr: map[string]string{"Authorization": "Bearer " + expert.Key}, hanodes: 200, config: 200},
		{name: "env key", hdr: map[string]string{"X-API-Key": envExportKey}, hanodes: 200, config: 403, errText: "у ключа нет права config"},
		{name: "revoked key", hdr: map[string]string{"X-API-Key": revoked.Key}, hanodes: 401, config: 401, errText: "нет доступа"},
		{name: "wrong key", hdr: map[string]string{"X-API-Key": "hp_wrong"}, hanodes: 401, config: 401, errText: "нет доступа"},
		{name: "nothing", hanodes: 401, config: 401, errText: "нет доступа"},
		{name: "jwt bearer", hdr: map[string]string{"Authorization": "Bearer " + jwt}, hanodes: 200, config: 200},
		{name: "jwt cookie", cookie: jwt, hanodes: 200, config: 200},
		{name: "legacy key with admin cookie", hdr: map[string]string{"X-API-Key": legacy.Key}, cookie: jwt, hanodes: 200, config: 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hdr := map[string]string{}
			for k, v := range tc.hdr {
				hdr[k] = v
			}
			if tc.cookie != "" {
				hdr["Cookie"] = auth.CookieName + "=" + tc.cookie
			}
			if rec := f.get(t, "/api/v1/hanodes", hdr); rec.Code != tc.hanodes {
				t.Fatalf("hanodes status=%d want %d body=%s", rec.Code, tc.hanodes, rec.Body.String())
			}
			rec := f.get(t, "/api/v1/config", hdr)
			if rec.Code != tc.config {
				t.Fatalf("config status=%d want %d body=%s", rec.Code, tc.config, rec.Body.String())
			}
			if tc.errText != "" {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != tc.errText {
					t.Fatalf("config error body=%s", rec.Body.String())
				}
			}
		})
	}

	keys, err := f.st.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.ID == expert.ID && k.LastUsedAt == nil {
			t.Fatal("config requests must update last_used_at")
		}
	}
}

// A successful /api/v1/config request stamps last_used_at of its key exactly
// like a /api/v1/hanodes request does; each path is checked on its own key.
func TestExportConfig_UpdatesLastUsedAt(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	viaConfig, err := f.st.CreateAPIKeyWithScopes("via-config", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}
	viaHanodes, err := f.st.CreateAPIKeyWithScopes("via-hanodes", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}
	unused, err := f.st.CreateAPIKeyWithScopes("unused", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}
	lastUsed := func() map[string]*time.Time {
		keys, err := f.st.ListAPIKeys()
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*time.Time{}
		for _, k := range keys {
			out[k.ID] = k.LastUsedAt
		}
		return out
	}
	for id, at := range lastUsed() {
		if at != nil {
			t.Fatalf("key %s used before any request: %v", id, at)
		}
	}

	start := time.Now().UTC().Truncate(time.Second)
	if rec := f.get(t, "/api/v1/config", map[string]string{"X-API-Key": viaConfig.Key}); rec.Code != http.StatusOK {
		t.Fatalf("config status=%d", rec.Code)
	}
	if rec := f.get(t, "/api/v1/hanodes", map[string]string{"X-API-Key": viaHanodes.Key}); rec.Code != http.StatusOK {
		t.Fatalf("hanodes status=%d", rec.Code)
	}
	// An admin session reads the export without touching any key.
	if rec := f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}); rec.Code != http.StatusOK {
		t.Fatalf("jwt config status=%d", rec.Code)
	}
	end := time.Now().UTC()

	got := lastUsed()
	for name, id := range map[string]string{"config": viaConfig.ID, "hanodes": viaHanodes.ID} {
		at := got[id]
		if at == nil {
			t.Fatalf("a successful %s request must update last_used_at", name)
		}
		if at.Before(start) || at.After(end) {
			t.Fatalf("%s last_used_at=%v outside [%v, %v]", name, at, start, end)
		}
	}
	if got[unused.ID] != nil {
		t.Fatalf("unused key last_used_at=%v", got[unused.ID])
	}
}

func TestExportConfig_NoSecretsNoUpstreamCallsNoWrites(t *testing.T) {
	var agentHits, remnaHits atomic.Int64
	agentSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { agentHits.Add(1) }))
	defer agentSrv.Close()
	remnaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { remnaHits.Add(1) }))
	defer remnaSrv.Close()
	f := newConfigFixture(t, agentSrv.URL+"/agent", remnaSrv.URL+"/canary-secret-path")
	key, err := f.st.CreateAPIKeyWithScopes("expert", []string{store.APIKeyScopeConfig})
	if err != nil {
		t.Fatal(err)
	}

	db := rawDB(t, f.path)
	stateDump := func() string {
		var b strings.Builder
		for _, q := range []string{
			`SELECT id || '|' || status || '|' || COALESCE(last_seen, '') || '|' || snapshot FROM nodes ORDER BY id`,
			`SELECT panel_id || '|' || remna_uuid || '|' || updated_at || '|' || users_online FROM remna_node_catalog ORDER BY panel_id, remna_uuid`,
		} {
			rows, err := db.Query(q)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var s string
				if err := rows.Scan(&s); err != nil {
					t.Fatal(err)
				}
				b.WriteString(s + "\n")
			}
			rows.Close()
		}
		return b.String()
	}
	before := stateDump()

	rec := f.get(t, "/api/v1/config", map[string]string{"X-API-Key": key.Key})
	_, secs := decodeConfig(t, rec)
	body := rec.Body.String()
	for _, c := range append(configCanaries, key.Key, store.HashAPIKey(key.Key), key.Prefix, agentSrv.URL, remnaSrv.URL) {
		if strings.Contains(body, c) {
			t.Errorf("config export leaks %q", c)
		}
	}
	if agentHits.Load() != 0 || remnaHits.Load() != 0 {
		t.Fatalf("config export called upstream: agent=%d remna=%d", agentHits.Load(), remnaHits.Load())
	}
	if after := stateDump(); after != before {
		t.Fatalf("config export changed state:\nbefore=%s\nafter=%s", before, after)
	}

	allowed := map[string][]string{
		"hanodes": {"id", "name", "country", "host", "sort_order", "created_at", "listen_ports", "entrances",
			"protect", "protect_default", "remna_panel_id", "provider_id", "provider_account_id", "traffic_log",
			"has_token", "backends", "backends_observed_at"},
		"links":             {"node_id", "backend", "server_name", "remna_panel_id", "remna_address", "remna_uuid", "match", "active"},
		"remna_panels":      {"id", "name", "has_api_key", "created_at"},
		"remna_nodes":       {"panel_id", "uuid", "name", "address", "config_profile_uuid", "inbound_uuids", "inbound_tags", "protocol", "protocol_derived", "protocol_override", "roles", "enabled_in_analytics", "notes", "alert_note"},
		"providers":         {"id", "name", "created_at"},
		"provider_accounts": {"id", "provider_id", "created_at"},
	}
	if len(secs) != len(allowed) {
		t.Fatalf("sections=%v", secs)
	}
	for name, keys := range allowed {
		sec, ok := secs[name]
		if !ok || sec.Status != "ok" || len(sec.Items) == 0 {
			t.Fatalf("section %s=%+v", name, sec)
		}
		want := append([]string(nil), keys...)
		sort.Strings(want)
		for _, item := range sec.Items {
			got := make([]string, 0, len(item))
			for k := range item {
				got = append(got, k)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("section %s item keys=%v want %v", name, got, want)
			}
		}
	}
}

func itemsByKey(items []map[string]any, key string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		v, _ := it[key].(string)
		out = append(out, v)
	}
	return out
}

func TestExportConfig_SectionsAndSorting(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	resp, secs := decodeConfig(t, f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}))
	if resp.Schema != "hapanel.config.v1" || resp.Version != "dev" {
		t.Fatalf("schema=%q version=%q", resp.Schema, resp.Version)
	}
	if _, err := time.Parse(time.RFC3339, resp.GeneratedAt); err != nil || strings.Contains(resp.GeneratedAt, ".") || !strings.HasSuffix(resp.GeneratedAt, "Z") {
		t.Fatalf("generated_at=%q", resp.GeneratedAt)
	}

	if got := itemsByKey(secs["hanodes"].Items, "id"); !reflect.DeepEqual(got, []string{cfgNode2, cfgNode1, cfgNode3}) {
		t.Fatalf("hanodes order=%v", got)
	}
	if got := itemsByKey(secs["remna_panels"].Items, "id"); !reflect.DeepEqual(got, []string{cfgPanelA, cfgPanelB}) {
		t.Fatalf("remna_panels order=%v", got)
	}
	if got := itemsByKey(secs["remna_nodes"].Items, "uuid"); !reflect.DeepEqual(got, []string{cfgExitA1, cfgExitA2, cfgExitA3, cfgExitB1}) {
		t.Fatalf("remna_nodes order=%v", got)
	}
	if got := itemsByKey(secs["providers"].Items, "id"); !reflect.DeepEqual(got, []string{cfgProv2, cfgProv1}) {
		t.Fatalf("providers order=%v", got)
	}
	if got := itemsByKey(secs["provider_accounts"].Items, "id"); !reflect.DeepEqual(got, []string{cfgAcc2, cfgAcc1}) {
		t.Fatalf("provider_accounts order=%v", got)
	}
	var linkOrder []string
	for _, l := range secs["links"].Items {
		linkOrder = append(linkOrder, l["node_id"].(string)+"/"+l["backend"].(string)+"/"+l["server_name"].(string))
	}
	wantLinks := []string{
		cfgNode2 + "/app/ru-exit",
		cfgNode1 + "/app/de-exit-2",
		cfgNode1 + "/app/nl-exit-1",
		cfgNode1 + "/reserve/old-exit",
		cfgOrphan + "/app/gone",
	}
	if !reflect.DeepEqual(linkOrder, wantLinks) {
		t.Fatalf("links order=%v", linkOrder)
	}

	byID := map[string]map[string]any{}
	for _, it := range secs["hanodes"].Items {
		byID[it["id"].(string)] = it
	}
	n1, n2, n3 := byID[cfgNode1], byID[cfgNode2], byID[cfgNode3]
	if n1["host"] != "203.0.113.10" || n2["host"] != "2001:db8::10" || n3["host"] != "" {
		t.Fatalf("hosts %v %v %v", n1["host"], n2["host"], n3["host"])
	}
	if n1["has_token"] != true || n3["has_token"] != false {
		t.Fatalf("has_token %v %v", n1["has_token"], n3["has_token"])
	}
	if n1["created_at"] != "2026-10-01T10:00:00Z" || n1["sort_order"] != float64(0) {
		t.Fatalf("n1 created_at=%v sort_order=%v", n1["created_at"], n1["sort_order"])
	}
	if n1["remna_panel_id"] != cfgPanelA || n1["provider_account_id"] != cfgAcc1 || n2["provider_id"] != nil || n3["remna_panel_id"] != nil {
		t.Fatalf("ids n1=%v/%v n2=%v n3=%v", n1["remna_panel_id"], n1["provider_account_id"], n2["provider_id"], n3["remna_panel_id"])
	}
	if !reflect.DeepEqual(n1["listen_ports"], []any{float64(443), float64(8443)}) {
		t.Fatalf("n1 listen_ports=%v", n1["listen_ports"])
	}
	if !reflect.DeepEqual(n2["entrances"], []any{map[string]any{"port": float64(8443), "backend": "app"}}) {
		t.Fatalf("n2 entrances=%v", n2["entrances"])
	}
	wantBackends := []any{
		map[string]any{"backend": "app", "name": "de-exit-2", "address": "198.51.100.22", "port": float64(8443)},
		map[string]any{"backend": "app", "name": "nl-exit-1", "address": "198.51.100.21", "port": float64(443)},
	}
	if !reflect.DeepEqual(n1["backends"], wantBackends) || n1["backends_observed_at"] != "2026-10-26T11:59:55Z" {
		t.Fatalf("n1 backends=%v at=%v", n1["backends"], n1["backends_observed_at"])
	}
	if !reflect.DeepEqual(n2["backends"], []any{}) || n2["backends_observed_at"] != nil {
		t.Fatalf("n2 backends=%v at=%v", n2["backends"], n2["backends_observed_at"])
	}
	if !reflect.DeepEqual(n3["backends"], []any{}) || n3["backends_observed_at"] != "2026-10-26T11:59:50Z" {
		t.Fatalf("n3 backends=%v at=%v", n3["backends"], n3["backends_observed_at"])
	}

	nodes := map[string]map[string]any{}
	for _, it := range secs["remna_nodes"].Items {
		nodes[it["uuid"].(string)] = it
	}
	a1, a2, a3 := nodes[cfgExitA1], nodes[cfgExitA2], nodes[cfgExitA3]
	if !reflect.DeepEqual(a1["inbound_tags"], []any{"VLESS_REALITY", "VLESS_XHTTP"}) || len(a1["inbound_uuids"].([]any)) != 2 {
		t.Fatalf("a1 inbound tags=%v uuids=%v", a1["inbound_tags"], a1["inbound_uuids"])
	}
	if a1["notes"] != "Exit behind <HP> front & CDN" || a1["alert_note"] != "Провайдер урезал канал" || a2["alert_note"] != "" {
		t.Fatalf("notes a1=%v/%v a2=%v", a1["notes"], a1["alert_note"], a2["alert_note"])
	}
	if a1["protocol"] != "vless_reality" || a2["protocol"] != "mixed" || a2["protocol_derived"] != "hysteria2" || a3["protocol"] != "unknown" {
		t.Fatalf("protocols a1=%v a2=%v/%v a3=%v", a1["protocol"], a2["protocol"], a2["protocol_derived"], a3["protocol"])
	}
	if !reflect.DeepEqual(a1["roles"], map[string]any{"rn_front": false, "rn_back": true, "hp_front": false, "hp_back": true, "cdn_back": false}) {
		t.Fatalf("a1 roles=%v", a1["roles"])
	}
	if !reflect.DeepEqual(a3["inbound_uuids"], []any{}) || !reflect.DeepEqual(a3["inbound_tags"], []any{}) {
		t.Fatalf("a3 inbound uuids=%v tags=%v", a3["inbound_uuids"], a3["inbound_tags"])
	}
	if nodes[cfgExitB1]["enabled_in_analytics"] != false || a1["enabled_in_analytics"] != true {
		t.Fatal("enabled_in_analytics")
	}

	panels := secs["remna_panels"].Items
	if panels[0]["has_api_key"] != true || panels[1]["has_api_key"] != false || panels[1]["created_at"] != "2026-09-02T00:00:00Z" {
		t.Fatalf("panels=%v", panels)
	}
}

func TestExportConfig_ProtectDefault(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	// Provisioning (api.go, opsagent) stores the default profile explicitly.
	provisioned, err := f.st.CreateNode("provisioned", "https://203.0.113.14:47893", "token-p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.SetNodeProtect(provisioned.ID, protect.Default()); err != nil {
		t.Fatal(err)
	}
	_, secs := decodeConfig(t, f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}))
	byID := map[string]map[string]any{}
	for _, it := range secs["hanodes"].Items {
		byID[it["id"].(string)] = it
	}
	def := map[string]any{
		"client_hello": true, "client_hello_delay_sec": float64(5), "rate_limit": true,
		"max_conn_per_ip": float64(100), "max_rate_per_ip": float64(60), "rate_window_sec": float64(10),
		"ru_only": false, "synproxy": false,
	}
	if n1 := byID[cfgNode1]; n1["protect_default"] != true || !reflect.DeepEqual(n1["protect"], def) {
		t.Fatalf("n1 protect=%v default=%v", n1["protect"], n1["protect_default"])
	}
	if n3 := byID[cfgNode3]; n3["protect_default"] != true || !reflect.DeepEqual(n3["protect"], def) {
		t.Fatalf("n3 protect=%v default=%v", n3["protect"], n3["protect_default"])
	}
	// protect_default means "the stored column is empty", not "profile equals the default".
	if p := byID[provisioned.ID]; p["protect_default"] != false || !reflect.DeepEqual(p["protect"], def) {
		t.Fatalf("explicit default profile: protect=%v protect_default=%v (want false)", p["protect"], p["protect_default"])
	}
	// Explicit profile, normalised like the panel (0 → default numeric).
	custom := map[string]any{
		"client_hello": false, "client_hello_delay_sec": float64(3), "rate_limit": true,
		"max_conn_per_ip": float64(50), "max_rate_per_ip": float64(60), "rate_window_sec": float64(20),
		"ru_only": true, "synproxy": true,
	}
	if n2 := byID[cfgNode2]; n2["protect_default"] != false || !reflect.DeepEqual(n2["protect"], custom) {
		t.Fatalf("n2 protect=%v default=%v", n2["protect"], n2["protect_default"])
	}
	// Same effective profile as the admin UI list.
	nodes, err := f.st.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		raw, _ := json.Marshal(n.Protect)
		var ui map[string]any
		_ = json.Unmarshal(raw, &ui)
		if !reflect.DeepEqual(byID[n.ID]["protect"], ui) {
			t.Fatalf("node %s export protect=%v ui=%v", n.ID, byID[n.ID]["protect"], ui)
		}
	}
}

func TestExportConfig_LinkMatchAndActive(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	_, secs := decodeConfig(t, f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}))
	got := map[string]map[string]any{}
	for _, l := range secs["links"].Items {
		got[l["node_id"].(string)+"/"+l["server_name"].(string)] = l
	}
	cases := []struct {
		key    string
		match  string
		uuid   any
		active bool
	}{
		{cfgNode1 + "/nl-exit-1", "unique", cfgExitA1, true}, // trim + case-insensitive, other panel ignored
		{cfgNode1 + "/de-exit-2", "ambiguous", nil, true},    // two catalog nodes share the address
		{cfgNode1 + "/old-exit", "none", nil, false},         // not in the node's saved backends
		{cfgNode2 + "/ru-exit", "unique", cfgExitB1, true},   // no saved backends: panel keeps every link
		{cfgOrphan + "/gone", "none", nil, false},            // node no longer exists
	}
	for _, tc := range cases {
		l, ok := got[tc.key]
		if !ok {
			t.Fatalf("missing link %s", tc.key)
		}
		if l["match"] != tc.match || l["remna_uuid"] != tc.uuid || l["active"] != tc.active {
			t.Errorf("link %s match=%v uuid=%v active=%v", tc.key, l["match"], l["remna_uuid"], l["active"])
		}
	}
	if l := got[cfgNode1+"/nl-exit-1"]; l["remna_address"] != " NL-Exit-1.example.net " || l["remna_panel_id"] != cfgPanelA {
		t.Fatalf("link stored values changed: %v", l)
	}
}

func TestExportConfig_FailingSection(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	db := rawDB(t, f.path)
	if _, err := db.Exec(`DROP TABLE providers`); err != nil {
		t.Fatal(err)
	}
	rec := f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)})
	_, secs := decodeConfig(t, rec)
	if p := secs["providers"]; p.Status != "error" || p.ContentHash != nil || p.Items == nil || len(p.Items) != 0 {
		t.Fatalf("providers=%+v", p)
	}
	if !strings.Contains(rec.Body.String(), `"providers":{"status":"error","items":[]}`) {
		t.Fatalf("error envelope: %s", rec.Body.String())
	}
	for _, name := range []string{"hanodes", "links", "remna_panels", "remna_nodes", "provider_accounts"} {
		if s := secs[name]; s.Status != "ok" || s.ContentHash == nil {
			t.Fatalf("section %s=%+v", name, s)
		}
	}

	// The catalog feeds link matching: without it links fail too, the rest stays ok.
	if _, err := db.Exec(`DROP TABLE remna_node_catalog`); err != nil {
		t.Fatal(err)
	}
	_, secs = decodeConfig(t, f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}))
	for _, name := range []string{"remna_nodes", "links", "providers"} {
		if secs[name].Status != "error" || len(secs[name].Items) != 0 {
			t.Fatalf("section %s=%+v", name, secs[name])
		}
	}
	for _, name := range []string{"hanodes", "remna_panels", "provider_accounts"} {
		if secs[name].Status != "ok" {
			t.Fatalf("section %s=%+v", name, secs[name])
		}
	}
}

func TestExportConfig_FailingHanodesFailsLinks(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	// One unreadable row makes ListNodeConfigs fail (scanNode parses created_at).
	if _, err := rawDB(t, f.path).Exec(`UPDATE nodes SET created_at = 'bad' WHERE id = ?`, cfgNode1); err != nil {
		t.Fatal(err)
	}
	rec := f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)})
	_, secs := decodeConfig(t, rec)
	// Without node snapshots links would come out "ok" with every link inactive.
	for _, name := range []string{"hanodes", "links"} {
		if s := secs[name]; s.Status != "error" || s.ContentHash != nil || s.Items == nil || len(s.Items) != 0 {
			t.Fatalf("section %s=%+v", name, s)
		}
	}
	if !strings.Contains(rec.Body.String(), `"links":{"status":"error","items":[]}`) {
		t.Fatalf("links error envelope: %s", rec.Body.String())
	}
	for _, name := range []string{"remna_panels", "remna_nodes", "providers", "provider_accounts"} {
		if s := secs[name]; s.Status != "ok" || s.ContentHash == nil || len(s.Items) == 0 {
			t.Fatalf("section %s=%+v", name, s)
		}
	}
}

// active is activeRemnaLinks over the node's saved snapshot (as the panel's
// online counts), not over the exported backends list, which hides management
// backends.
func TestExportConfig_LinkActiveRule(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	const mgmtOnly = "3f000000-0000-4000-8000-0000000000aa"
	db := rawDB(t, f.path)
	for _, q := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO nodes (id, name, url, token, created_at, status, snapshot) VALUES (?, 'mgmt-only', 'https://203.0.113.15:47893', '',
  '2026-10-04T00:00:00Z', 'online', '{"backends":[{"backend":"hap_agent","name":"agent","address":"127.0.0.1","port":9100}],"updated_at":"2026-10-26T11:59:40Z"}')`,
			[]any{mgmtOnly}},
		// cfgNode3 has a saved snapshot without any backends.
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'any-exit', ?, '192.0.2.10')`,
			[]any{cfgNode3, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'exit', ?, '192.0.2.11')`,
			[]any{mgmtOnly, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'hap_agent', 'agent', ?, '192.0.2.12')`,
			[]any{mgmtOnly, cfgPanelA}},
		{`INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address) VALUES (?, 'app', 'NL-EXIT-1', ?, '192.0.2.13')`,
			[]any{cfgNode1, cfgPanelA}},
	} {
		if _, err := db.Exec(q.q, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	_, secs := decodeConfig(t, f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)}))
	nodes := map[string]map[string]any{}
	for _, n := range secs["hanodes"].Items {
		nodes[n["id"].(string)] = n
	}
	for _, id := range []string{cfgNode3, mgmtOnly} {
		if n := nodes[id]; !reflect.DeepEqual(n["backends"], []any{}) || n["backends_observed_at"] == nil {
			t.Fatalf("node %s backends=%v at=%v", id, n["backends"], n["backends_observed_at"])
		}
	}
	got := map[string]any{}
	for _, l := range secs["links"].Items {
		got[l["node_id"].(string)+"/"+l["backend"].(string)+"/"+l["server_name"].(string)] = l["active"]
	}
	for key, want := range map[string]bool{
		cfgNode3 + "/app/any-exit":    true,  // saved snapshot has no backends: every link is active
		cfgNode2 + "/app/ru-exit":     true,  // no saved snapshot: every link is active
		mgmtOnly + "/app/exit":        false, // snapshot holds only a management backend
		mgmtOnly + "/hap_agent/agent": true,  // management backends count for active
		cfgNode1 + "/app/NL-EXIT-1":   false, // backend and server name compare exactly
		cfgNode1 + "/app/nl-exit-1":   true,
		cfgOrphan + "/app/gone":       false, // node no longer exists
	} {
		if got[key] != want {
			t.Errorf("link %s active=%v want %v", key, got[key], want)
		}
	}
}

// remna_uuid/match use the panel's address key: a link matches a catalog node
// exactly when remna.FindNodeByAddress would find that node for the link.
func TestConfigLinkItems_AddressMatchingFollowsPanel(t *testing.T) {
	addrs := []string{
		"nl-exit-1.example.net", " NL-Exit-1.example.net ", "nl-exit-1.example.net:443",
		"198.51.100.22", "198.51.100.22\t", "\t198.51.100.22", "[2001:db8::10]", "[2001:DB8::10]", "", "  ",
	}
	for _, linkAddr := range addrs {
		for _, catalogAddr := range addrs {
			links := []store.BackendRemnaLink{{NodeID: "n", Backend: "app", ServerName: "s", RemnaPanelID: "p", RemnaAddress: linkAddr}}
			catalog := []store.RemnaNodeConfig{{PanelID: "p", RemnaUUID: "u", Address: catalogAddr}}
			got := configLinkItems(links, nil, catalog)
			panelHit := remna.FindNodeByAddress([]remna.Node{{UUID: "u", Address: catalogAddr}}, linkAddr) != nil
			if len(got) != 1 || (got[0].Match == "unique") != panelHit || (got[0].RemnaUUID != nil) != panelHit {
				t.Errorf("link %q catalog %q: export=%+v panel hit=%v", linkAddr, catalogAddr, got, panelHit)
			}
			if !panelHit && got[0].Match != "none" {
				t.Errorf("link %q catalog %q: match=%q want none", linkAddr, catalogAddr, got[0].Match)
			}
		}
	}
	// Two catalog nodes with the same key: the panel takes the first, the export reports it.
	links := []store.BackendRemnaLink{{NodeID: "n", Backend: "app", ServerName: "s", RemnaPanelID: "p", RemnaAddress: "198.51.100.22"}}
	catalog := []store.RemnaNodeConfig{{PanelID: "p", RemnaUUID: "u1", Address: "198.51.100.22"}, {PanelID: "p", RemnaUUID: "u2", Address: " 198.51.100.22"}}
	if got := configLinkItems(links, nil, catalog); got[0].Match != "ambiguous" || got[0].RemnaUUID != nil {
		t.Fatalf("ambiguous=%+v", got)
	}
}

// The README documents the canonical form as Python's
//
//	json.dumps(v, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
//	    .replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
//
// encoded as UTF-8. The literals below are that recipe's output (checked with
// Python 3): Go's canonicalJSON must give the same bytes and hashes.
func TestCanonicalJSON_DocumentedForm(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want string
		hash string
	}{
		{
			name: "line and paragraph separators",
			v:    []map[string]any{{"notes": "line\u2028sep <b>&"}},
			want: `[{"notes":"line\u2028sep <b>&"}]`,
			hash: "sha256:a552f0d7199cd5228366d8e5a933cad4317ee6c0cd42888195344c43ac4e0214",
		},
		{
			name: "escapes, HTML and non-ASCII text",
			v: []map[string]any{{
				"notes":      "a\u2028b\u2029c",
				"alert_note": "Провайдер урезал канал 🚀",
				"ctl":        "\b\f\n\r\t\x01\x1f\x7f",
				"quote":      `"\ / <HP> & CDN`,
			}},
			want: `[{"alert_note":"Провайдер урезал канал 🚀","ctl":"\b\f\n\r\t\u0001\u001f` + "\x7f" +
				`","notes":"a\u2028b\u2029c","quote":"\"\\ / <HP> & CDN"}]`,
			hash: "sha256:ff8b15f857c8f8c34de52251f20b4d03551f9be0b6709c9dc0fb379a212b6591",
		},
		{
			name: "keys sorted at every level, integers as written",
			v:    map[string]any{"b": []any{map[string]any{"z": 1, "a": int64(-20)}}, "a": map[string]any{}, "c": []int{}, "d": nil},
			want: `{"a":{},"b":[{"a":-20,"z":1}],"c":[],"d":null}`,
			hash: "sha256:443f76c0d9aec1a0e3e5ea1508cbac80d6e9b77b1f6c1337e6cda1d90b19eef5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := canonicalJSON(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("canonical JSON\n got %q\nwant %q", got, tc.want)
			}
			if h := sha256Tag([]byte(tc.want)); h != tc.hash {
				t.Fatalf("hash of the documented form=%s want %s", h, tc.hash)
			}
		})
	}
}

// End to end: section and snapshot hashes of a response whose free text has
// U+2028/U+2029 equal SHA-256 of the documented canonical bytes.
func TestExportConfig_HashMatchesDocumentedRecipe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := rawDB(t, path).Exec(`INSERT INTO remna_node_catalog (panel_id, remna_uuid, name, address, protocol_derived, notes, alert_note)
VALUES ('p1', 'u1', 'exit <1> & co', 'exit.example.net', 'vless_reality', ?, ?)`, "line\u2028sep", "p\u2029q"); err != nil {
		t.Fatal(err)
	}
	au := auth.New("admin-pass", jwtSecret, time.Hour)
	srv := New(st, au, agent.New(true), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{})
	tok, _, err := au.IssueToken()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	resp, secs := decodeConfig(t, rec)

	remnaNodes := `[{"address":"exit.example.net","alert_note":"p\u2029q","config_profile_uuid":"","enabled_in_analytics":true,` +
		`"inbound_tags":[],"inbound_uuids":[],"name":"exit <1> & co","notes":"line\u2028sep","panel_id":"p1","protocol":"vless_reality",` +
		`"protocol_derived":"vless_reality","protocol_override":"","roles":{"cdn_back":false,"hp_back":false,"hp_front":false,"rn_back":false,"rn_front":false},"uuid":"u1"}]`
	emptyHash := sha256Tag([]byte(`[]`))
	if emptyHash != "sha256:4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatalf("hash of [] = %s", emptyHash)
	}
	if h := *secs["remna_nodes"].ContentHash; h != sha256Tag([]byte(remnaNodes)) {
		t.Fatalf("remna_nodes content_hash=%s want %s", h, sha256Tag([]byte(remnaNodes)))
	}
	empty := func(name string) string {
		return `"` + name + `":{"content_hash":"` + emptyHash + `","items":[],"status":"ok"}`
	}
	sections := `{` + empty("hanodes") + `,` + empty("links") + `,` + empty("provider_accounts") + `,` + empty("providers") +
		`,"remna_nodes":{"content_hash":"` + sha256Tag([]byte(remnaNodes)) + `","items":` + remnaNodes + `,"status":"ok"},` +
		empty("remna_panels") + `}`
	if resp.SnapshotHash != sha256Tag([]byte(sections)) {
		t.Fatalf("snapshot_hash=%s want %s", resp.SnapshotHash, sha256Tag([]byte(sections)))
	}
	if resp.SnapshotHash != "sha256:8273edb42c126a709b6cf574851533cc05847ef03b42e4bed3402bb1d6df9c90" {
		t.Fatalf("snapshot_hash=%s differs from the value computed with the documented Python recipe", resp.SnapshotHash)
	}
}

func TestExportConfig_HashesAndETag(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	hdr := map[string]string{"Authorization": "Bearer " + f.jwt(t)}
	rec := f.get(t, "/api/v1/config", hdr)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q", ct)
	}
	resp, _ := decodeConfig(t, rec)
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(resp.SnapshotHash) {
		t.Fatalf("snapshot_hash=%q", resp.SnapshotHash)
	}
	if etag := rec.Header().Get("ETag"); etag != `"`+resp.SnapshotHash+`"` {
		t.Fatalf("etag=%q hash=%q", etag, resp.SnapshotHash)
	}

	// Hashes are reproducible from the response: canonical JSON (sorted keys,
	// no whitespace, no HTML escaping) of items and of the sections object.
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.UseNumber()
	var generic map[string]any
	if err := dec.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	sections := generic["sections"].(map[string]any)
	for name, raw := range sections {
		sec := raw.(map[string]any)
		canon, err := canonicalJSON(sec["items"])
		if err != nil {
			t.Fatal(err)
		}
		if sec["content_hash"] != sha256Tag(canon) {
			t.Fatalf("section %s content_hash=%v want %s", name, sec["content_hash"], sha256Tag(canon))
		}
	}
	canon, err := canonicalJSON(sections)
	if err != nil {
		t.Fatal(err)
	}
	if resp.SnapshotHash != sha256Tag(canon) {
		t.Fatalf("snapshot_hash=%s want %s", resp.SnapshotHash, sha256Tag(canon))
	}

	// Unchanged data → same hash; a config change → new hash.
	again, _ := decodeConfig(t, f.get(t, "/api/v1/config", hdr))
	if again.SnapshotHash != resp.SnapshotHash {
		t.Fatal("snapshot_hash must be stable for unchanged data")
	}
	if err := f.st.SetRemnaNodeAlertNote(cfgPanelA, cfgExitA2, "новая заметка"); err != nil {
		t.Fatal(err)
	}
	changed, secs := decodeConfig(t, f.get(t, "/api/v1/config", hdr))
	if changed.SnapshotHash == resp.SnapshotHash {
		t.Fatal("snapshot_hash must change with the configuration")
	}
	var before map[string]sectionResponse
	_, before = decodeConfig(t, rec)
	if *secs["hanodes"].ContentHash != *before["hanodes"].ContentHash || *secs["remna_nodes"].ContentHash == *before["remna_nodes"].ContentHash {
		t.Fatal("only the changed section hash may change")
	}
}

func TestExportConfig_GoldenExample(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	rec := f.get(t, "/api/v1/config", map[string]string{"Authorization": "Bearer " + f.jwt(t)})
	decodeConfig(t, rec)
	body := regexp.MustCompile(`"generated_at":"[^"]*"`).ReplaceAll(rec.Body.Bytes(), []byte(`"generated_at":"2026-10-26T12:00:00Z"`))
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "config_v1_example.json")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/api -run Golden -update)", err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Fatalf("config export differs from %s; rerun with -update and review the diff", golden)
	}
	for _, c := range configCanaries {
		if bytes.Contains(want, []byte(c)) {
			t.Fatalf("golden example contains %q", c)
		}
	}
}

func TestCreateAPIKey_ScopesAndLegacyBody(t *testing.T) {
	f := newConfigFixture(t, "https://203.0.113.10:47893", "https://canary-base-url.example/canary-secret-path")
	jwt := f.jwt(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/settings/api-keys", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+jwt)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		f.srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	type created struct {
		Key struct {
			ID        string   `json:"id"`
			Name      string   `json:"name"`
			Prefix    string   `json:"prefix"`
			CreatedAt string   `json:"created_at"`
			Revoked   *bool    `json:"revoked"`
			Key       string   `json:"key"`
			Scopes    []string `json:"scopes"`
		} `json:"key"`
		Warning string `json:"warning"`
	}

	// The current UI sends only a name: legacy key, same response as before plus "scopes": [].
	rec := post(`{"name":"ui-key"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var legacy created
	if err := json.Unmarshal(rec.Body.Bytes(), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Key.Name != "ui-key" || !strings.HasPrefix(legacy.Key.Key, "hp_") || legacy.Key.Prefix != legacy.Key.Key[:11] ||
		legacy.Key.ID == "" || legacy.Key.CreatedAt == "" || legacy.Key.Revoked == nil || *legacy.Key.Revoked || legacy.Warning == "" {
		t.Fatalf("legacy create=%+v", legacy)
	}
	if legacy.Key.Scopes == nil || len(legacy.Key.Scopes) != 0 {
		t.Fatalf("legacy scopes=%#v", legacy.Key.Scopes)
	}
	if rec := post(``); rec.Code != http.StatusCreated {
		t.Fatalf("empty body status=%d", rec.Code)
	}
	if rec := f.get(t, "/api/v1/hanodes", map[string]string{"X-API-Key": legacy.Key.Key}); rec.Code != http.StatusOK {
		t.Fatalf("legacy key hanodes=%d", rec.Code)
	}
	if rec := f.get(t, "/api/v1/config", map[string]string{"X-API-Key": legacy.Key.Key}); rec.Code != http.StatusForbidden {
		t.Fatalf("legacy key config=%d", rec.Code)
	}

	rec = post(`{"name":"vpn-expert","scopes":["config"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("scoped create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var scoped created
	if err := json.Unmarshal(rec.Body.Bytes(), &scoped); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(scoped.Key.Scopes, []string{"config"}) {
		t.Fatalf("scoped scopes=%v", scoped.Key.Scopes)
	}
	if rec := f.get(t, "/api/v1/config", map[string]string{"X-API-Key": scoped.Key.Key}); rec.Code != http.StatusOK {
		t.Fatalf("scoped key config=%d", rec.Code)
	}

	if rec := post(`{"name":"bad","scopes":["admin"]}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "неизвестное право") {
		t.Fatalf("unknown scope status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := post(`{"name":"bad","scopes":"config"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("non-list scopes status=%d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/api-keys", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	listRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(listRec, req)
	var list struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Keys) != 3 {
		t.Fatalf("keys=%v", list.Keys)
	}
	for _, k := range list.Keys {
		scopes, ok := k["scopes"].([]any)
		if !ok {
			t.Fatalf("key without scopes list: %v", k)
		}
		if k["id"] == scoped.Key.ID && !reflect.DeepEqual(scopes, []any{"config"}) {
			t.Fatalf("listed scoped key=%v", k)
		}
		if _, leaked := k["key"]; leaked {
			t.Fatal("list must not return plaintext keys")
		}
	}

	// API keys never open admin routes, whatever their scopes.
	adminReq := httptest.NewRequest(http.MethodGet, "/api/settings/api-keys", nil)
	adminReq.Header.Set("Authorization", "Bearer "+scoped.Key.Key)
	adminRec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusUnauthorized {
		t.Fatalf("scoped key on admin route=%d", adminRec.Code)
	}
}
