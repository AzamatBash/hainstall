package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// testdataBase: index.txt with an article before the first section, a title
// with parentheses, an invalid key, a duplicate, a missing file, a line
// without a key and an unlisted file; articles with HTML, quotes, a tab,
// U+2028, CRLF and no final newline. A warning shows no key that fails the
// article key form.
const testdataBase = "testdata/base"

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func mustLoad(t *testing.T, dir string) *Base {
	t.Helper()
	b, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	return b
}

// writeBase writes index.txt and files (paths relative to the base) into a
// new directory.
func writeBase(t *testing.T, index string, files map[string]string) string {
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

// copyBase copies testdata/base into a new directory a test may change.
func copyBase(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(testdataBase, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(testdataBase, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func editFile(t *testing.T, path string, edit func(string) string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(edit(string(data))), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad_Testdata(t *testing.T) {
	b := mustLoad(t, testdataBase)
	if b.Title != "Тестовая база" {
		t.Fatalf("title=%q", b.Title)
	}
	wants := []struct{ key, title, section string }{
		{"a/00-intro.txt", "Вступление без раздела", ""},
		{"a/10-first.txt", "Первая статья", "Первый раздел"},
		{"a/11-second.txt", "Статья (со скобками) в заголовке", "Первый раздел"},
		{"a/20-last.txt", "Последняя статья", "Второй раздел"},
	}
	if len(b.Articles) != len(wants) {
		t.Fatalf("articles=%+v", b.Articles)
	}
	for i, w := range wants {
		a := b.Articles[i]
		raw, err := os.ReadFile(filepath.Join(testdataBase, filepath.FromSlash(w.key)))
		if err != nil {
			t.Fatal(err)
		}
		if a.Key != w.key || a.Title != w.title || a.Section != w.section || a.Order != i+1 {
			t.Fatalf("article %d=%+v want %+v order %d", i, a, w, i+1)
		}
		if a.Size != len(raw) || a.SHA256 != sha256Hex(raw) || a.Text != string(raw) {
			t.Fatalf("article %s: size=%d sha256=%s, file has %d bytes and %s; text must be the file bytes as is",
				w.key, a.Size, a.SHA256, len(raw), sha256Hex(raw))
		}
		if got, ok := b.Article(w.key); !ok || got != a {
			t.Fatalf("Article(%s)=%+v,%v", w.key, got, ok)
		}
	}
	wantWarnings := []Warning{
		{WarnInvalidKey, ""}, // a/Bad_Name.txt
		{WarnDuplicateKey, "a/10-first.txt"},
		{WarnListedButMissing, "a/12-missing.txt"},
		{WarnInvalidKey, ""},
		{WarnUnlistedFile, "a/30-unlisted.txt"},
	}
	if !reflect.DeepEqual(b.Warnings, wantWarnings) {
		t.Fatalf("warnings=%+v\nwant %+v", b.Warnings, wantWarnings)
	}
	for _, key := range []string{"a/Bad_Name.txt", "a/12-missing.txt", "a/30-unlisted.txt", "index.txt", "a/../index.txt"} {
		if _, ok := b.Article(key); ok {
			t.Fatalf("%s must not be served", key)
		}
	}
	// The duplicate line does not replace the first one.
	if a, _ := b.Article("a/10-first.txt"); a.Title != "Первая статья" || a.Order != 2 {
		t.Fatalf("first line must stay: %+v", a)
	}
}

// The revision is SHA-256 of the documented canonical form, written out here
// by hand: sorted keys, no whitespace, text as is.
func TestLoad_RevisionIsCanonicalTableOfContents(t *testing.T) {
	b := mustLoad(t, testdataBase)
	sha := func(key string) string {
		a, ok := b.Article(key)
		if !ok {
			t.Fatalf("no %s", key)
		}
		return a.SHA256
	}
	canonical := `{"articles":[` +
		`{"key":"a/00-intro.txt","order":1,"section":"","sha256":"` + sha("a/00-intro.txt") + `","title":"Вступление без раздела"},` +
		`{"key":"a/10-first.txt","order":2,"section":"Первый раздел","sha256":"` + sha("a/10-first.txt") + `","title":"Первая статья"},` +
		`{"key":"a/11-second.txt","order":3,"section":"Первый раздел","sha256":"` + sha("a/11-second.txt") + `","title":"Статья (со скобками) в заголовке"},` +
		`{"key":"a/20-last.txt","order":4,"section":"Второй раздел","sha256":"` + sha("a/20-last.txt") + `","title":"Последняя статья"}` +
		`],"schema":"hapanel.kb.v1","title":"Тестовая база"}`
	if b.Revision != sha256Hex([]byte(canonical)) {
		t.Fatalf("revision=%s want sha256 of %s", b.Revision, canonical)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(b.Revision) {
		t.Fatalf("revision=%q", b.Revision)
	}
}

func TestLoad_RevisionFollowsTheTableOfContentsOnly(t *testing.T) {
	orig := mustLoad(t, testdataBase)
	cases := []struct {
		name string
		edit func(t *testing.T, dir string)
		same bool
	}{
		{name: "copy of the same base", edit: func(t *testing.T, dir string) {}, same: true},
		{name: "unlisted file added", same: true, edit: func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "a", "40-new.txt"), []byte("новая"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unrelated INDEX.txt changed", same: true, edit: func(t *testing.T, dir string) {
			if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
				return // INDEX.txt is index.txt on a case-insensitive file system
			}
			if err := os.WriteFile(filepath.Join(dir, "INDEX.txt"), []byte("старый перечень"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "article text", edit: func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "a", "11-second.txt"), func(s string) string { return s + "#" })
		}},
		{name: "article title", edit: func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "index.txt"), func(s string) string {
				return strings.Replace(s, "- Первая статья (", "- Первая статья, правка (", 1)
			})
		}},
		{name: "section name", edit: func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "index.txt"), func(s string) string {
				return strings.Replace(s, "Второй раздел", "Другой раздел", 1)
			})
		}},
		{name: "order", edit: func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "index.txt"), func(s string) string {
				s = strings.Replace(s, "- Первая статья (a/10-first.txt)\n", "", 1)
				return strings.Replace(s, "- Файла нет (a/12-missing.txt)\n", "- Файла нет (a/12-missing.txt)\n- Первая статья (a/10-first.txt)\n", 1)
			})
		}},
		{name: "base title", edit: func(t *testing.T, dir string) {
			editFile(t, filepath.Join(dir, "index.txt"), func(s string) string {
				return strings.Replace(s, "Тестовая база", "Другая база", 1)
			})
		}},
		{name: "missing article appears", edit: func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, "a", "12-missing.txt"), []byte("появилась"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyBase(t)
			tc.edit(t, dir)
			b := mustLoad(t, dir)
			if (b.Revision == orig.Revision) != tc.same {
				t.Fatalf("revision %s, original %s, same=%v", b.Revision, orig.Revision, tc.same)
			}
		})
	}
}

func TestLoad_IndexParsing(t *testing.T) {
	bom := string(rune(0xFEFF))
	nbsp := string(rune(0x00A0))
	index := bom + "\r\n  \r\n" +
		"Заголовок:   База с BOM и CRLF  \r\n" +
		"- Первая (a/a.txt)\r\n" +
		"Раздел\t\r\n" +
		"-\tВторая" + nbsp + "(a/b.txt)  \r\n" +
		"Заголовок: только первая строка называет базу\r\n" +
		"- Третья (a/c.txt)\r\n" +
		"- Без пробела перед скобкой(a/d.txt)\r\n"
	dir := writeBase(t, index, map[string]string{"a/a.txt": "A", "a/b.txt": "B", "a/c.txt": "C"})
	b := mustLoad(t, dir)
	if b.Title != "База с BOM и CRLF" {
		t.Fatalf("title=%q", b.Title)
	}
	got := make([]string, 0, len(b.Articles))
	for _, a := range b.Articles {
		got = append(got, fmt.Sprintf("%d|%s|%s|%s", a.Order, a.Key, a.Title, a.Section))
	}
	want := []string{
		"1|a/a.txt|Первая|",
		"2|a/b.txt|Вторая|Раздел",
		"3|a/c.txt|Третья|Раздел", // a later "Заголовок:" line is skipped, as in the UI
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("articles=%q\nwant %q", got, want)
	}
	if want := []Warning{{WarnInvalidKey, ""}}; !reflect.DeepEqual(b.Warnings, want) {
		t.Fatalf("warnings=%+v want %+v", b.Warnings, want)
	}

	// No title line: the base has no title and the first line is a section.
	b = mustLoad(t, writeBase(t, "Раздел\n- Статья (a/a.txt)\n", map[string]string{"a/a.txt": "A"}))
	if b.Title != "" || len(b.Articles) != 1 || b.Articles[0].Section != "Раздел" {
		t.Fatalf("title=%q articles=%+v", b.Title, b.Articles)
	}
}

func TestValidKey(t *testing.T) {
	long := strings.Repeat("a", 151)
	for key, want := range map[string]bool{
		"a/01-glossary.txt":     true,
		"a/x.txt":               true,
		"a/0.txt":               true,
		"a/a..b_c-d.txt":        true,
		"a/" + long + ".txt":    true,
		"a/" + long + "a.txt":   false,
		"a/.hidden.txt":         false,
		"a/-dash.txt":           false,
		"a/../index.txt":        false,
		"a/sub/x.txt":           false,
		"a/X.txt":               false,
		"a/x y.txt":             false,
		"a/x.TXT":               false,
		"a/x.md":                false,
		"a/.txt":                false,
		"a/x.txt\n":             false,
		"/etc/passwd":           false,
		"b/x.txt":               false,
		"x.txt":                 false,
		"a\\x.txt":              false,
		"":                      false,
		" a/x.txt":              false,
		"a/статья.txt":          false,
		"a/x.txt/../../etc.txt": false,
	} {
		if got := ValidKey(key); got != want {
			t.Errorf("ValidKey(%q)=%v want %v", key, got, want)
		}
	}
}

func TestLoad_SymbolicLinksAreNotFollowed(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("CANARY-OUTSIDE-THE-BASE"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink: %v", err)
		}
	}
	noCanary := func(t *testing.T, b *Base) {
		t.Helper()
		for _, a := range b.Articles {
			if strings.Contains(a.Text, "CANARY") {
				t.Fatalf("article %s was read through a link", a.Key)
			}
		}
	}

	t.Run("article files", func(t *testing.T) {
		dir := writeBase(t, "Заголовок: Ссылки\n- Наружу (a/out.txt)\n- Внутрь базы (a/alias.txt)\n- Обычная (a/plain.txt)\n",
			map[string]string{"a/plain.txt": "обычный текст"})
		symlink(t, outside, filepath.Join(dir, "a", "out.txt"))
		symlink(t, "plain.txt", filepath.Join(dir, "a", "alias.txt"))
		symlink(t, outside, filepath.Join(dir, "a", "zz-unlisted.txt"))
		b := mustLoad(t, dir)
		noCanary(t, b)
		if len(b.Articles) != 1 || b.Articles[0].Key != "a/plain.txt" || b.Articles[0].Order != 1 {
			t.Fatalf("articles=%+v", b.Articles)
		}
		want := []Warning{
			{WarnListedButMissing, "a/out.txt"},
			{WarnListedButMissing, "a/alias.txt"},
			{WarnUnlistedFile, "a/zz-unlisted.txt"},
		}
		if !reflect.DeepEqual(b.Warnings, want) {
			t.Fatalf("warnings=%+v want %+v", b.Warnings, want)
		}
	})

	t.Run("articles directory", func(t *testing.T) {
		real := writeBase(t, "Заголовок: x\n", map[string]string{"a/plain.txt": "CANARY-OUTSIDE-THE-BASE"})
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte("Заголовок: Каталог-ссылка\n- Обычная (a/plain.txt)\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, filepath.Join(real, "a"), filepath.Join(dir, "a"))
		b := mustLoad(t, dir)
		noCanary(t, b)
		if len(b.Articles) != 0 || !reflect.DeepEqual(b.Warnings, []Warning{{WarnListedButMissing, "a/plain.txt"}}) {
			t.Fatalf("articles=%+v warnings=%+v", b.Articles, b.Warnings)
		}
	})

	t.Run("index.txt", func(t *testing.T) {
		real := writeBase(t, "Заголовок: x\n- Обычная (a/plain.txt)\n", map[string]string{"a/plain.txt": "текст"})
		dir := t.TempDir()
		symlink(t, filepath.Join(real, "index.txt"), filepath.Join(dir, "index.txt"))
		if _, err := Load(dir); !errors.Is(err, ErrNotFound) {
			t.Fatalf("index.txt behind a link: err=%v, want ErrNotFound", err)
		}
	})

	t.Run("base directory itself", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "kb")
		abs, err := filepath.Abs(testdataBase)
		if err != nil {
			t.Fatal(err)
		}
		symlink(t, abs, link)
		if b := mustLoad(t, link); b.Revision != mustLoad(t, testdataBase).Revision {
			t.Fatal("PANEL_KB_DIR may be a link: same base, same revision")
		}
	})
}

func TestLoad_ArticleLimits(t *testing.T) {
	exact := strings.Repeat("я", MaxArticleSize/2) // 2 bytes per letter
	files := map[string]string{
		"a/exact.txt":     exact,
		"a/large.txt":     exact + ".",
		"a/latin1.txt":    string([]byte{'c', 'a', 'f', 0xe9}),
		"a/truncated.txt": string([]byte{'o', 'k', 0xd0}),
		"a/ok.txt":        "обычная",
	}
	index := "Заголовок: Пределы\n" +
		"- Ровно 4 МиБ (a/exact.txt)\n" +
		"- Больше 4 МиБ (a/large.txt)\n" +
		"- Latin-1 (a/latin1.txt)\n" +
		"- Обрезанный символ (a/truncated.txt)\n" +
		"- Обычная (a/ok.txt)\n"
	b := mustLoad(t, writeBase(t, index, files))
	if len(b.Articles) != 2 {
		t.Fatalf("articles=%d", len(b.Articles))
	}
	if a := b.Articles[0]; a.Key != "a/exact.txt" || a.Size != MaxArticleSize || a.Order != 1 || len(a.Text) != MaxArticleSize {
		t.Fatalf("exactly 4 MiB must be served: key=%s size=%d order=%d", a.Key, a.Size, a.Order)
	}
	if a := b.Articles[1]; a.Key != "a/ok.txt" || a.Order != 2 {
		t.Fatalf("second=%s order=%d", a.Key, a.Order)
	}
	want := []Warning{
		{WarnArticleTooLarge, "a/large.txt"},
		{WarnInvalidUTF8, "a/latin1.txt"},
		{WarnInvalidUTF8, "a/truncated.txt"},
	}
	if !reflect.DeepEqual(b.Warnings, want) {
		t.Fatalf("warnings=%+v want %+v", b.Warnings, want)
	}
}

func TestLoad_TooManyArticles(t *testing.T) {
	var index strings.Builder
	index.WriteString("Заголовок: Много статей\n")
	files := map[string]string{}
	for i := 1; i <= MaxArticles+2; i++ {
		key := fmt.Sprintf("a/%03d.txt", i)
		fmt.Fprintf(&index, "- Статья %d (%s)\n", i, key)
		files[key] = fmt.Sprintf("статья %d", i)
	}
	b := mustLoad(t, writeBase(t, index.String(), files))
	if len(b.Articles) != MaxArticles {
		t.Fatalf("articles=%d want %d", len(b.Articles), MaxArticles)
	}
	if last := b.Articles[MaxArticles-1]; last.Key != "a/500.txt" || last.Order != MaxArticles {
		t.Fatalf("last=%s order=%d", last.Key, last.Order)
	}
	want := []Warning{{WarnTooManyArticles, "a/501.txt"}, {WarnTooManyArticles, "a/502.txt"}}
	if !reflect.DeepEqual(b.Warnings, want) {
		t.Fatalf("warnings=%+v want %+v", b.Warnings, want)
	}
}

func TestLoad_NoBase(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(indexDir, "index.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{
		"not configured":           "",
		"missing directory":        filepath.Join(t.TempDir(), "missing"),
		"no index.txt":             t.TempDir(),
		"a file, not a directory":  file,
		"index.txt is a directory": indexDir,
	} {
		if _, err := Load(dir); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err=%v, want ErrNotFound", name, err)
		}
	}
	// index.txt alone is a base: its articles are missing.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte("Заголовок: Без статей\n- Статья (a/x.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := mustLoad(t, dir)
	if b.Title != "Без статей" || len(b.Articles) != 0 || !reflect.DeepEqual(b.Warnings, []Warning{{WarnListedButMissing, "a/x.txt"}}) {
		t.Fatalf("base=%+v", b)
	}
}

// A broken index.txt or an article that cannot be read fails the whole load
// (not ErrNotFound): a reader of the table of contents must never take an
// unreadable article for a removed one.
func TestLoad_ReadErrorsFailTheLoad(t *testing.T) {
	notUTF8 := writeBase(t, "Заголовок: x\n- A (a/a.txt)\n"+string([]byte{0xff}), map[string]string{"a/a.txt": "A"})
	large := writeBase(t, "Заголовок: x\n"+strings.Repeat("#", maxIndexSize), nil)
	for name, dir := range map[string]string{"index.txt not UTF-8": notUTF8, "index.txt over 1 MiB": large} {
		if _, err := Load(dir); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err=%v", name, err)
		}
	}

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not stop this user from reading")
	}
	dir := writeBase(t, "Заголовок: x\n- A (a/a.txt)\n- B (a/b.txt)\n", map[string]string{"a/a.txt": "A", "a/b.txt": "B"})
	if err := os.Chmod(filepath.Join(dir, "a", "b.txt"), 0); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "a/b.txt") {
		t.Fatalf("unreadable article: err=%v", err)
	}
}

func TestLoader_LoadsOnceAndRetriesFailures(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "kb")
	l := NewLoader(dir, nil)
	if l.Dir() != dir {
		t.Fatalf("dir=%q", l.Dir())
	}
	if _, err := l.Base(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no base yet: err=%v", err)
	}

	// The failure was not kept: the base is read once it exists.
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte("Заголовок: x\n- A (a/a.txt)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "a.txt"), []byte("первая редакция"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := l.Base()
	if err != nil {
		t.Fatal(err)
	}

	// Kept until restart: later changes on disk are not seen.
	if err := os.WriteFile(filepath.Join(dir, "a", "a.txt"), []byte("вторая редакция"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "index.txt")); err != nil {
		t.Fatal(err)
	}
	again, err := l.Base()
	if err != nil || again != first {
		t.Fatalf("second call: base=%p err=%v, want %p", again, err, first)
	}
	if a, _ := again.Article("a/a.txt"); a.Text != "первая редакция" {
		t.Fatalf("text=%q", a.Text)
	}
}

func TestLoader_ConcurrentFirstUse(t *testing.T) {
	l := NewLoader(testdataBase, nil)
	const n = 32
	var wg sync.WaitGroup
	start := make(chan struct{})
	got := make([]*Base, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], errs[i] = l.Base()
		}(i)
	}
	close(start)
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil || got[i] == nil || got[i] != got[0] {
			t.Fatalf("caller %d: base=%p err=%v, first=%p", i, got[i], errs[i], got[0])
		}
	}
}

// TestLoad_CheckDir checks a real base, e.g. before building an image:
//
//	KB_CHECK_DIR=../../../kb go test ./internal/kb -run CheckDir -v
//
// Any warning fails it, and so does an article line the UI bundle builder
// reads otherwise (with node installed). On macOS take panel/kb from git (git
// archive): there INDEX.txt and index.txt are one file in the working tree.
func TestLoad_CheckDir(t *testing.T) {
	dir := os.Getenv("KB_CHECK_DIR")
	if dir == "" {
		t.Skip("KB_CHECK_DIR is not set")
	}
	b := mustLoad(t, dir)
	sections := map[string]bool{}
	for _, a := range b.Articles {
		sections[a.Section] = true
	}
	t.Logf("title=%q articles=%d sections=%d revision=%s", b.Title, len(b.Articles), len(sections), b.Revision)
	for _, w := range b.Warnings {
		t.Errorf("warning %s %q", w.Code, w.Key)
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ui, skip := uiBuilderParse(t, []string{string(index)})
	if ui == nil {
		t.Log(skip)
		return
	}
	if got := articleLines(string(index)); !reflect.DeepEqual(got, ui[0]) {
		t.Errorf("the UI bundle builder reads index.txt otherwise:\nloader %q\nUI     %q", got, ui[0])
	} else {
		t.Logf("the UI bundle builder reads the same %d article lines", len(got))
	}
}
