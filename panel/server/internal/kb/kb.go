// Package kb loads the knowledge base shipped in the panel image: the same
// panel/kb the UI bundle is built from, copied to PANEL_KB_DIR (/app/kb). The
// base is index.txt (title, sections, article lines) and the article files
// a/<name>.txt. GET /api/v1/kb and GET /api/v1/kb/article serve it as schema
// hapanel.kb.v1.
package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/azabash/hapanel/panel/internal/canonjson"
)

// Schema names the export format; it is part of the revision.
const Schema = "hapanel.kb.v1"

// Limits. Articles over them are left out with a warning; a larger index.txt
// fails the load.
const (
	MaxArticles    = 500
	MaxArticleSize = 4 << 20 // 4 MiB
	maxIndexSize   = 1 << 20
)

// Warning codes. What does not pass the checks is left out of the table of
// contents and reported with one of these.
const (
	WarnInvalidKey       = "invalid_key"        // article line without a key of the article key form
	WarnDuplicateKey     = "duplicate_key"      // key listed again; the first line stays
	WarnListedButMissing = "listed_but_missing" // no regular file for the key (missing, symbolic link, directory)
	WarnUnlistedFile     = "unlisted_file"      // file in a/ that index.txt does not list; not served
	WarnArticleTooLarge  = "article_too_large"  // file over MaxArticleSize
	WarnInvalidUTF8      = "invalid_utf8"       // file is not valid UTF-8
	WarnTooManyArticles  = "too_many_articles"  // listed after MaxArticles served articles
)

// ErrNotFound means there is no base: the directory or its index.txt is
// missing. An index.txt that is a symbolic link or not a regular file counts
// as missing.
var ErrNotFound = errors.New("knowledge base not found")

var (
	keyPattern = regexp.MustCompile(`^a/[a-z0-9][a-z0-9._-]{0,150}\.txt$`)
	// "- <title> (<key>)": the key is the last parenthesised group of the line,
	// so titles may contain parentheses. Separators are any spaces, as in the
	// UI bundle builder (panel/web/scripts/build-kb.mjs).
	articleLine = regexp.MustCompile(`^-[\s\pZ]+(.+?)[\s\pZ]+\(([^()]*)\)$`)

	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("file too large")
)

const titlePrefix = "Заголовок:"

// Article is one served article.
type Article struct {
	Key     string // "a/<name>.txt", the path given in index.txt
	Title   string // from index.txt
	Section string // "" for articles before the first section
	Order   int    // position in the table of contents, from 1
	Size    int    // bytes of the file
	SHA256  string // lowercase hex SHA-256 of the file bytes
	Text    string // the file bytes as is (valid UTF-8)
}

// Warning reports what was left out and why.
type Warning struct {
	Code string
	Key  string // "" when an article line has no key in parentheses
}

// Base is a loaded knowledge base. It never changes after Load.
type Base struct {
	Title    string    // from the "Заголовок: …" line of index.txt
	Revision string    // lowercase hex SHA-256 of the canonical table of contents
	Articles []Article // table of contents order
	Warnings []Warning // index.txt order, then unlisted files by name
	byKey    map[string]int
}

// ValidKey reports whether key has the article key form
// ^a/[a-z0-9][a-z0-9._-]{0,150}\.txt$.
func ValidKey(key string) bool {
	return keyPattern.MatchString(key)
}

// Article returns the served article with key.
func (b *Base) Article(key string) (Article, bool) {
	i, ok := b.byKey[key]
	if !ok {
		return Article{}, false
	}
	return b.Articles[i], true
}

// Load reads the base in dir (dir itself may be a symbolic link; nothing
// inside it is followed).
//
// index.txt: the first non-empty line "Заголовок: <title>" names the base; a
// line starting with "-" is an article line "- <title> (a/<name>.txt)"; any
// other non-empty line names the section of the articles below it; articles
// before the first section get the empty section.
//
// Errors: ErrNotFound when there is no directory or index.txt. Any other error
// (a file that cannot be read, index.txt over 1 MiB or not UTF-8) fails the
// whole load: an article that could not be read is never reported as absent.
func Load(dir string) (*Base, error) {
	if dir == "" {
		return nil, ErrNotFound
	}
	if fi, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	} else if !fi.IsDir() {
		return nil, ErrNotFound
	}
	index, err := readRegular(filepath.Join(dir, "index.txt"), maxIndexSize)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
		return nil, ErrNotFound
	case errors.Is(err, errTooLarge):
		return nil, fmt.Errorf("index.txt is larger than %d bytes", maxIndexSize)
	case err != nil:
		return nil, err
	}
	if !utf8.Valid(index) {
		return nil, errors.New("index.txt is not valid UTF-8")
	}

	articlesDir := filepath.Join(dir, "a")
	hasArticlesDir := false
	if fi, err := os.Lstat(articlesDir); err == nil {
		hasArticlesDir = fi.IsDir() // a symbolic link to a directory is not followed
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	b := &Base{Articles: []Article{}, Warnings: []Warning{}, byKey: map[string]int{}}
	warn := func(code, key string) {
		b.Warnings = append(b.Warnings, Warning{Code: code, Key: key})
	}
	listed := map[string]bool{} // every key an article line names, valid or not
	section := ""
	first := true
	for _, line := range strings.Split(strings.TrimPrefix(string(index), "\uFEFF"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, titlePrefix) {
				b.Title = strings.TrimSpace(strings.TrimPrefix(line, titlePrefix))
				continue
			}
		}
		if !strings.HasPrefix(line, "-") {
			section = line
			continue
		}
		m := articleLine.FindStringSubmatch(line)
		if m == nil {
			warn(WarnInvalidKey, "")
			continue
		}
		title, key := strings.TrimSpace(m[1]), m[2]
		again := listed[key]
		listed[key] = true
		switch {
		case !ValidKey(key):
			warn(WarnInvalidKey, key)
			continue
		case again:
			warn(WarnDuplicateKey, key)
			continue
		case len(b.Articles) >= MaxArticles:
			warn(WarnTooManyArticles, key)
			continue
		case !hasArticlesDir:
			warn(WarnListedButMissing, key)
			continue
		}
		// The key form keeps the path inside a/: one name, no leading dot.
		data, err := readRegular(filepath.Join(dir, filepath.FromSlash(key)), MaxArticleSize)
		switch {
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, errNotRegular):
			warn(WarnListedButMissing, key)
			continue
		case errors.Is(err, errTooLarge):
			warn(WarnArticleTooLarge, key)
			continue
		case err != nil:
			return nil, fmt.Errorf("read %s: %w", key, err)
		case !utf8.Valid(data):
			warn(WarnInvalidUTF8, key)
			continue
		}
		sum := sha256.Sum256(data)
		b.byKey[key] = len(b.Articles)
		b.Articles = append(b.Articles, Article{
			Key:     key,
			Title:   title,
			Section: section,
			Order:   len(b.Articles) + 1,
			Size:    len(data),
			SHA256:  hex.EncodeToString(sum[:]),
			Text:    string(data),
		})
	}

	if hasArticlesDir {
		entries, err := os.ReadDir(articlesDir) // sorted by name
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if key := "a/" + e.Name(); !listed[key] {
				warn(WarnUnlistedFile, key)
			}
		}
	}

	if b.Revision, err = revision(b.Title, b.Articles); err != nil {
		return nil, err
	}
	return b, nil
}

// revision is SHA-256 of the canonical JSON (canonjson) of
// {"articles":[{"key","order","section","sha256","title"}…],"schema","title"}.
// It changes only with the base, never with the build version or the time.
func revision(title string, articles []Article) (string, error) {
	type item struct {
		Key     string `json:"key"`
		Order   int    `json:"order"`
		Section string `json:"section"`
		SHA256  string `json:"sha256"`
		Title   string `json:"title"`
	}
	items := make([]item, 0, len(articles))
	for _, a := range articles {
		items = append(items, item{Key: a.Key, Order: a.Order, Section: a.Section, SHA256: a.SHA256, Title: a.Title})
	}
	raw, err := canonjson.Marshal(struct {
		Articles []item `json:"articles"`
		Schema   string `json:"schema"`
		Title    string `json:"title"`
	}{Articles: items, Schema: Schema, Title: title})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// readRegular reads path when it is a regular file, not a symbolic link, of
// at most limit bytes. Errors: fs.ErrNotExist when missing; errNotRegular for
// a symbolic link, directory or other special file; errTooLarge over limit.
func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if info.Size() > limit {
		return nil, errTooLarge
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// The file opened must be the one checked, not a link put in its place.
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errNotRegular
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

// Loader reads the base once, on first use, and keeps it until the process
// exits: the base changes only with the image. A failed load is not kept, so
// a later call reads again. Safe for concurrent use.
type Loader struct {
	dir    string
	logger *slog.Logger
	mu     sync.Mutex
	base   atomic.Pointer[Base]
}

// NewLoader returns a loader of the base in dir; logger may be nil.
func NewLoader(dir string, logger *slog.Logger) *Loader {
	return &Loader{dir: dir, logger: logger}
}

// Dir is the base directory.
func (l *Loader) Dir() string {
	return l.dir
}

// Base returns the base, loading it on the first successful call.
func (l *Loader) Base() (*Base, error) {
	if b := l.base.Load(); b != nil {
		return b, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if b := l.base.Load(); b != nil {
		return b, nil
	}
	b, err := Load(l.dir)
	if err != nil {
		return nil, err
	}
	l.base.Store(b)
	if l.logger != nil {
		l.logger.Info("kb loaded", "dir", l.dir, "articles", len(b.Articles), "warnings", len(b.Warnings), "revision", b.Revision)
		if len(b.Warnings) > 0 {
			l.logger.Warn("kb warnings", "count", len(b.Warnings), "first", warningSummary(b.Warnings, 20))
		}
	}
	return b, nil
}

// warningSummary lists up to n warnings as "code key; …" for the log.
func warningSummary(ws []Warning, n int) string {
	parts := make([]string, 0, min(len(ws), n))
	for _, w := range ws {
		if len(parts) == n {
			break
		}
		parts = append(parts, strings.TrimSpace(w.Code+" "+w.Key))
	}
	return strings.Join(parts, "; ")
}
