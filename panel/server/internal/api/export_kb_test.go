package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/kb"
	"github.com/azabash/hapanel/panel/internal/store"
)

const kbTestIndex = "Заголовок: Заметки по настройке\n" +
	"\n" +
	"- Вступление (a/00-intro.txt)\n" +
	"\n" +
	"Раздел\n" +
	"- Статья с разметкой (a/10-markup.txt)\n" +
	"- Нет файла (a/11-missing.txt)\n"

// Text that JSON escapes in different ways: HTML, quotes, backslash, tab,
// U+2028, CRLF, emoji; no final newline in the first article.
var kbTestFiles = map[string]string{
	"a/00-intro.txt":    "Заголовок: Вступление\n\nБез завершающего перевода строки.",
	"a/10-markup.txt":   "Заголовок: Статья с разметкой\r\n\r\n<b>HTML</b> & кавычки \"как есть\", \\ черта,\tтабуляция" + string(rune(0x2028)) + "разделитель 🚀\r\n",
	"a/20-unlisted.txt": "Не в оглавлении",
}

func writeKBDir(t *testing.T, index string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, text := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

type kbFixture struct {
	st  *store.Store
	au  *auth.Service
	srv *Server
}

func newKBFixture(t *testing.T, dir string) *kbFixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	au := auth.New("admin-pass", jwtSecret, time.Hour)
	srv := New(st, au, agent.New(true), slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		APIKey: envExportKey,
		KBDir:  dir,
	})
	return &kbFixture{st: st, au: au, srv: srv}
}

func (f *kbFixture) get(t *testing.T, target string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (f *kbFixture) admin(t *testing.T) map[string]string {
	t.Helper()
	tok, _, err := f.au.IssueToken()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"Authorization": "Bearer " + tok}
}

type kbManifestBody struct {
	Schema      string `json:"schema"`
	Version     string `json:"version"`
	GeneratedAt string `json:"generated_at"`
	Revision    string `json:"revision"`
	Title       string `json:"title"`
	Articles    []struct {
		Key     string `json:"key"`
		Title   string `json:"title"`
		Section string `json:"section"`
		Order   int    `json:"order"`
		Size    int    `json:"size"`
		SHA256  string `json:"sha256"`
	} `json:"articles"`
	Warnings []struct {
		Code string `json:"code"`
		Key  string `json:"key"`
	} `json:"warnings"`
}

type kbArticleBody struct {
	Schema   string `json:"schema"`
	Version  string `json:"version"`
	Revision string `json:"revision"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	SHA256   string `json:"sha256"`
	Size     int    `json:"size"`
	Text     string `json:"text"`
}

func decodeKB[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func kbSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, text string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status=%d want %d body=%s", rec.Code, status, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["error"] != text {
		t.Fatalf("body=%s want error %q", rec.Body.String(), text)
	}
}

func wantNoStore(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q (status %d)", cc, rec.Code)
	}
}

func TestKB_Access(t *testing.T) {
	f := newKBFixture(t, writeKBDir(t, kbTestIndex, kbTestFiles))
	legacy, err := f.st.CreateAPIKey("legacy")
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := f.st.CreateAPIKeyWithScopes("vpn-expert", []string{store.APIKeyScopeConfig})
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
	jwt := f.admin(t)["Authorization"]

	cases := []struct {
		name    string
		hdr     map[string]string
		status  int
		errText string
	}{
		{name: "key with config", hdr: map[string]string{"X-API-Key": scoped.Key}, status: 200},
		{name: "key with config, bearer", hdr: map[string]string{"Authorization": "Bearer " + scoped.Key}, status: 200},
		{name: "admin jwt", hdr: map[string]string{"Authorization": jwt}, status: 200},
		{name: "admin cookie", hdr: map[string]string{"Cookie": auth.CookieName + "=" + strings.TrimPrefix(jwt, "Bearer ")}, status: 200},
		{name: "key without config", hdr: map[string]string{"X-API-Key": legacy.Key}, status: 403, errText: "у ключа нет права config"},
		{name: "PANEL_API_KEY", hdr: map[string]string{"X-API-Key": envExportKey}, status: 403, errText: "у ключа нет права config"},
		{name: "revoked key", hdr: map[string]string{"X-API-Key": revoked.Key}, status: 401, errText: "нет доступа"},
		{name: "wrong key", hdr: map[string]string{"X-API-Key": "hp_wrong"}, status: 401, errText: "нет доступа"},
		{name: "no key", status: 401, errText: "нет доступа"},
	}
	for _, tc := range cases {
		for _, target := range []string{"/api/v1/kb", "/api/v1/kb/article?key=a/00-intro.txt"} {
			t.Run(tc.name+" "+target, func(t *testing.T) {
				rec := f.get(t, target, tc.hdr)
				wantNoStore(t, rec)
				if tc.errText != "" {
					wantError(t, rec, tc.status, tc.errText)
					if rec.Header().Get("ETag") != "" {
						t.Fatal("an error has no ETag")
					}
					return
				}
				if rec.Code != tc.status {
					t.Fatalf("status=%d want %d body=%s", rec.Code, tc.status, rec.Body.String())
				}
			})
		}
	}

	keys, err := f.st.ListAPIKeys()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if k.ID == scoped.ID && k.LastUsedAt == nil {
			t.Fatal("knowledge base requests must update last_used_at like the config export")
		}
	}
}

func TestKB_Manifest(t *testing.T) {
	dir := writeKBDir(t, kbTestIndex, kbTestFiles)
	f := newKBFixture(t, dir)
	rec := f.get(t, "/api/v1/kb", f.admin(t))
	wantNoStore(t, rec)
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type=%q", ct)
	}

	// Exactly the fields of the contract.
	var generic map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &generic); err != nil {
		t.Fatal(err)
	}
	if got, want := sortedKeys(generic), []string{"articles", "generated_at", "revision", "schema", "title", "version", "warnings"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest keys=%v want %v", got, want)
	}
	for _, it := range generic["articles"].([]any) {
		if got, want := sortedKeys(it.(map[string]any)), []string{"key", "order", "section", "sha256", "size", "title"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("article keys=%v want %v", got, want)
		}
	}
	for _, it := range generic["warnings"].([]any) {
		if got, want := sortedKeys(it.(map[string]any)), []string{"code", "key"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("warning keys=%v want %v", got, want)
		}
	}

	m := decodeKB[kbManifestBody](t, rec)
	if m.Schema != "hapanel.kb.v1" || m.Version != "dev" || m.Title != "Заметки по настройке" {
		t.Fatalf("schema=%q version=%q title=%q", m.Schema, m.Version, m.Title)
	}
	if _, err := time.Parse(time.RFC3339, m.GeneratedAt); err != nil || strings.Contains(m.GeneratedAt, ".") || !strings.HasSuffix(m.GeneratedAt, "Z") {
		t.Fatalf("generated_at=%q", m.GeneratedAt)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(m.Revision) {
		t.Fatalf("revision=%q", m.Revision)
	}
	if etag := rec.Header().Get("ETag"); etag != `"`+m.Revision+`"` {
		t.Fatalf("etag=%q revision=%q", etag, m.Revision)
	}
	loaded, err := kb.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Revision != loaded.Revision {
		t.Fatalf("revision=%s, loader gives %s", m.Revision, loaded.Revision)
	}

	type article struct {
		Key, Title, Section string
		Order, Size         int
		SHA256              string
	}
	var got []article
	for _, a := range m.Articles {
		got = append(got, article{a.Key, a.Title, a.Section, a.Order, a.Size, a.SHA256})
	}
	intro, markup := kbTestFiles["a/00-intro.txt"], kbTestFiles["a/10-markup.txt"]
	want := []article{
		{"a/00-intro.txt", "Вступление", "", 1, len(intro), kbSHA256(intro)},
		{"a/10-markup.txt", "Статья с разметкой", "Раздел", 2, len(markup), kbSHA256(markup)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("articles=%+v\nwant %+v", got, want)
	}
	var warnings []string
	for _, w := range m.Warnings {
		warnings = append(warnings, w.Code+" "+w.Key)
	}
	if want := []string{"listed_but_missing a/11-missing.txt", "unlisted_file a/20-unlisted.txt"}; !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings=%q want %q", warnings, want)
	}

	// No 304: a matching If-None-Match still gets the full answer.
	hdr := f.admin(t)
	hdr["If-None-Match"] = rec.Header().Get("ETag")
	again := f.get(t, "/api/v1/kb", hdr)
	if m2 := decodeKB[kbManifestBody](t, again); m2.Revision != m.Revision || len(m2.Articles) != 2 {
		t.Fatalf("second answer=%+v", m2)
	}
}

func TestKB_EmptyListsAreArrays(t *testing.T) {
	f := newKBFixture(t, writeKBDir(t, "Заголовок: Пусто\n", nil))
	rec := f.get(t, "/api/v1/kb", f.admin(t))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"articles":[]`) || !strings.Contains(rec.Body.String(), `"warnings":[]`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestKB_Article(t *testing.T) {
	f := newKBFixture(t, writeKBDir(t, kbTestIndex, kbTestFiles))
	m := decodeKB[kbManifestBody](t, f.get(t, "/api/v1/kb", f.admin(t)))

	for _, target := range []string{"/api/v1/kb/article?key=a/10-markup.txt", "/api/v1/kb/article?key=a%2F10-markup.txt"} {
		rec := f.get(t, target, f.admin(t))
		wantNoStore(t, rec)
		var generic map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &generic); err != nil {
			t.Fatalf("%s: %v body=%s", target, err, rec.Body.String())
		}
		if got, want := sortedKeys(generic), []string{"key", "revision", "schema", "sha256", "size", "text", "title", "version"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("article keys=%v want %v", got, want)
		}
		a := decodeKB[kbArticleBody](t, rec)
		text := kbTestFiles["a/10-markup.txt"]
		if a.Text != text {
			t.Fatalf("text must be the file bytes as is:\n got %q\nwant %q", a.Text, text)
		}
		if a.Schema != "hapanel.kb.v1" || a.Version != "dev" || a.Revision != m.Revision || a.Key != "a/10-markup.txt" ||
			a.Title != "Статья с разметкой" || a.Size != len(text) || a.SHA256 != kbSHA256(text) || a.SHA256 != m.Articles[1].SHA256 {
			t.Fatalf("article=%+v", a)
		}
	}
}

func TestKB_ArticleErrors(t *testing.T) {
	f := newKBFixture(t, writeKBDir(t, kbTestIndex, kbTestFiles))
	cases := []struct {
		query   string
		status  int
		errText string
	}{
		{"", 400, "не указан ключ статьи"},
		{"?key=", 400, "не указан ключ статьи"},
		{"?other=a/00-intro.txt", 400, "не указан ключ статьи"},
		{"?key=a/../index.txt", 400, "неверный ключ статьи"},
		{"?key=index.txt", 400, "неверный ключ статьи"},
		{"?key=/etc/passwd", 400, "неверный ключ статьи"},
		{"?key=a/Intro.txt", 400, "неверный ключ статьи"},
		{"?key=a/00-intro.txt&key=a/10-markup.txt", 400, "неверный ключ статьи"},
		{"?key=a/99-unknown.txt", 404, "статья не найдена"},
		{"?key=a/11-missing.txt", 404, "статья не найдена"},  // listed, no file
		{"?key=a/20-unlisted.txt", 404, "статья не найдена"}, // file, not listed
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			rec := f.get(t, "/api/v1/kb/article"+tc.query, f.admin(t))
			wantNoStore(t, rec)
			wantError(t, rec, tc.status, tc.errText)
		})
	}
}

func TestKB_NoBase(t *testing.T) {
	for name, dir := range map[string]string{
		"missing directory": filepath.Join(t.TempDir(), "missing"),
		"not configured":    "",
		"no index.txt":      t.TempDir(),
	} {
		t.Run(name, func(t *testing.T) {
			f := newKBFixture(t, dir)
			for _, target := range []string{"/api/v1/kb", "/api/v1/kb/article?key=a/00-intro.txt"} {
				rec := f.get(t, target, f.admin(t))
				wantNoStore(t, rec)
				wantError(t, rec, http.StatusNotFound, "база знаний недоступна")
				if rec.Header().Get("ETag") != "" {
					t.Fatal("no ETag without a base")
				}
			}
			// The request is checked before the base, access before both.
			wantError(t, f.get(t, "/api/v1/kb/article?key=bad", f.admin(t)), http.StatusBadRequest, "неверный ключ статьи")
			wantError(t, f.get(t, "/api/v1/kb", nil), http.StatusUnauthorized, "нет доступа")
		})
	}
}

// The base is read once and kept until restart: it changes only with the image.
func TestKB_ReadOnceUntilRestart(t *testing.T) {
	dir := writeKBDir(t, kbTestIndex, kbTestFiles)
	f := newKBFixture(t, dir)
	before := decodeKB[kbManifestBody](t, f.get(t, "/api/v1/kb", f.admin(t)))

	if err := os.WriteFile(filepath.Join(dir, "a", "00-intro.txt"), []byte("изменено после запуска"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte("Заголовок: Другая\n- Вступление (a/00-intro.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := decodeKB[kbManifestBody](t, f.get(t, "/api/v1/kb", f.admin(t)))
	if after.Revision != before.Revision || after.Title != before.Title || len(after.Articles) != len(before.Articles) {
		t.Fatalf("base changed without a restart: %+v", after)
	}
	a := decodeKB[kbArticleBody](t, f.get(t, "/api/v1/kb/article?key=a/00-intro.txt", f.admin(t)))
	if a.Text != kbTestFiles["a/00-intro.txt"] {
		t.Fatalf("text=%q", a.Text)
	}

	restarted := newKBFixture(t, dir)
	if m := decodeKB[kbManifestBody](t, restarted.get(t, "/api/v1/kb", restarted.admin(t))); m.Revision == before.Revision || m.Title != "Другая" {
		t.Fatalf("after restart: revision=%s title=%q", m.Revision, m.Title)
	}
}

// A base that cannot be read answers 500 and is read again on the next request.
func TestKB_ReadErrorIsRetried(t *testing.T) {
	dir := writeKBDir(t, "Заголовок: x\n- A (a/a.txt)\n"+string([]byte{0xff, 0xfe}), map[string]string{"a/a.txt": "A"})
	f := newKBFixture(t, dir)
	rec := f.get(t, "/api/v1/kb", f.admin(t))
	wantNoStore(t, rec)
	wantError(t, rec, http.StatusInternalServerError, "ошибка чтения базы знаний")

	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte("Заголовок: x\n- A (a/a.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m := decodeKB[kbManifestBody](t, f.get(t, "/api/v1/kb", f.admin(t))); len(m.Articles) != 1 {
		t.Fatalf("articles=%+v", m.Articles)
	}
}
