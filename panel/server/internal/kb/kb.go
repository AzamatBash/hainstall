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
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/azabash/hapanel/panel/internal/canonjson"
	"golang.org/x/sys/unix"
)

// Schema names the export format; it is part of the revision.
const Schema = "hapanel.kb.v1"

// Limits. Articles over them are left out with a warning; a larger index.txt
// fails the load.
const (
	MaxArticles    = 500
	MaxArticleSize = 4 << 20 // 4 MiB
	// MaxArticleJSONSize bounds the text of an article written as a JSON
	// string (escapes counted, quotes not), so that the article response
	// stays within the 10 MiB Panel VPN reads. Only control characters other
	// than \b \f \n \r \t, six bytes each as \u00xx, can take a text of
	// MaxArticleSize past it.
	MaxArticleJSONSize = 2 * MaxArticleSize
	// MaxLineLength bounds a served title or section, in code points.
	MaxLineLength = 300
	// MaxWarnings bounds the warnings served: past it the first
	// MaxWarnings-1 are kept and the last one is too_many_warnings.
	MaxWarnings  = 100
	maxIndexSize = 1 << 20
)

// Warning codes. What does not pass the checks is left out of the table of
// contents and reported with one of these; only empty_title reports an
// article that is served.
const (
	WarnInvalidKey       = "invalid_key"        // article key not of the article key form, or a "-" line that is not an article line
	WarnDuplicateKey     = "duplicate_key"      // key listed again; the first line stays
	WarnListedButMissing = "listed_but_missing" // no regular file for the key (missing, symbolic link, directory)
	WarnUnlistedFile     = "unlisted_file"      // file in a/ that index.txt does not list; not served
	WarnArticleTooLarge  = "article_too_large"  // file over MaxArticleSize, or its text over MaxArticleJSONSize in JSON
	WarnInvalidUTF8      = "invalid_utf8"       // file is not valid UTF-8
	WarnTooManyArticles  = "too_many_articles"  // listed after MaxArticles served articles
	WarnEmptyTitle       = "empty_title"        // served, titled with its file name: the title is empty once cleaned
	WarnTooManyWarnings  = "too_many_warnings"  // last of MaxWarnings served warnings: the rest were left out
)

// ErrNotFound means there is no base: the directory or its index.txt is
// missing. An index.txt that is a symbolic link or not a regular file counts
// as missing.
var ErrNotFound = errors.New("knowledge base not found")

// jsSpace is the white space and the line terminators of JavaScript, the
// characters of \s and of String.prototype.trim (isJSSpace), as the body of
// a character class.
const jsSpace = `\t\n\v\f\r \x{00A0}\x{1680}\x{2000}-\x{200A}\x{2028}\x{2029}\x{202F}\x{205F}\x{3000}\x{FEFF}`

var (
	keyPattern = regexp.MustCompile(`^a/[a-z0-9][a-z0-9._-]{0,150}\.txt$`)
	// articleLine is the article line of the UI bundle builder (parseIndex in
	// panel/web/scripts/build-kb.mjs), /^-\s+(.+?)\s+\((a\/[^)]+\.txt)\)\s*$/,
	// with JavaScript's \s (jsSpace) and "." (any character but \n, \r, U+2028
	// and U+2029) spelled out. Go picks the match JavaScript picks
	// (leftmost-first): the title is the shortest text before a "(a/….txt)"
	// that ends the line, so titles may contain parentheses.
	articleLine = regexp.MustCompile(`^-[` + jsSpace + `]+([^\n\r\x{2028}\x{2029}]+?)[` + jsSpace + `]+\((a/[^)]+\.txt)\)[` + jsSpace + `]*$`)

	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("file too large")
)

const titlePrefix = "Заголовок:"

// Article is one served article.
type Article struct {
	Key     string // "a/<name>.txt", the path given in index.txt
	Title   string // from index.txt, cleaned; the file name when that is empty
	Section string // cleaned; "" for articles before the first section
	Order   int    // position in the table of contents, from 1
	Size    int    // bytes of the file
	SHA256  string // lowercase hex SHA-256 of the file bytes
	Text    string // the file bytes as is (valid UTF-8)
}

// Warning reports what was left out and why.
type Warning struct {
	Code string
	Key  string // only a key of the article key form; "" otherwise
}

// Base is a loaded knowledge base. It never changes after Load.
type Base struct {
	Title    string    // from the "Заголовок: …" line of index.txt, cleaned
	Revision string    // lowercase hex SHA-256 of the canonical table of contents
	Articles []Article // table of contents order
	Warnings []Warning // index.txt order, then unlisted files by name; at most MaxWarnings
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
// index.txt is read the way the UI bundle builder reads it (parseIndex), so
// the API lists the articles, titles and sections the UI shows, less the
// articles that fail the checks of the key, the file, its size and UTF-8.
// Titles and sections are served cleaned (cleanLine); an article whose
// title is empty once cleaned is titled with its file name (a/<name>.txt
// gives <name>) and reported as empty_title.
//
// A warning shows a key only when it has the article key form, and at most
// MaxWarnings are kept.
//
// Errors: ErrNotFound when there is no directory or index.txt. Any other error
// (a file that cannot be read, index.txt over 1 MiB or not UTF-8) fails the
// whole load: an article that could not be read is never reported as absent.
func Load(dir string) (*Base, error) {
	if dir == "" {
		return nil, ErrNotFound
	}
	// Follow the configured root once; all children are then opened relative
	// to pinned directories. A concurrent rename cannot redirect a read.
	rootFD, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, unix.ENOTDIR) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	root := os.NewFile(uintptr(rootFD), dir)
	defer root.Close()
	index, err := readRegularAt(root, "index.txt", maxIndexSize)
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

	var articlesDir *os.File
	articlesFD, err := unix.Openat(rootFD, "a", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err == nil {
		articlesDir = os.NewFile(uintptr(articlesFD), "a")
		defer articlesDir.Close()
	} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, unix.ENOTDIR) && !errors.Is(err, unix.ELOOP) {
		return nil, err
	}
	hasArticlesDir := articlesDir != nil

	b := &Base{Articles: []Article{}, Warnings: []Warning{}, byKey: map[string]int{}}
	warnings := 0
	warn := func(code, key string) {
		warnings++
		if len(b.Warnings) == MaxWarnings {
			return
		}
		if !ValidKey(key) {
			key = ""
		}
		b.Warnings = append(b.Warnings, Warning{Code: code, Key: key})
	}
	var lines []indexLine
	b.Title, lines = parseIndex(string(index))
	listed := map[string]bool{} // every key an article line names, valid or not
	for _, line := range lines {
		key := line.key
		if key == "" {
			warn(WarnInvalidKey, "")
			continue
		}
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
		// The key form gives a single child name, opened within the pinned a/.
		data, err := readRegularAt(articlesDir, strings.TrimPrefix(key, "a/"), MaxArticleSize)
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
		case jsonStringSize(data) > MaxArticleJSONSize:
			warn(WarnArticleTooLarge, key)
			continue
		}
		title := line.title
		if title == "" {
			title = strings.TrimSuffix(strings.TrimPrefix(key, "a/"), ".txt")
			warn(WarnEmptyTitle, key)
		}
		sum := sha256.Sum256(data)
		b.byKey[key] = len(b.Articles)
		b.Articles = append(b.Articles, Article{
			Key:     key,
			Title:   title,
			Section: line.section,
			Order:   len(b.Articles) + 1,
			Size:    len(data),
			SHA256:  hex.EncodeToString(sum[:]),
			Text:    string(data),
		})
	}

	if hasArticlesDir {
		names, err := articlesDir.Readdirnames(-1)
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		for _, name := range names {
			// Do not let DirEntry's unknown-type fallback stat through a
			// pathname: classification must use the same pinned directory.
			var info unix.Stat_t
			if err := unix.Fstatat(articlesFD, name, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if info.Mode&unix.S_IFMT == unix.S_IFDIR {
				continue
			}
			if key := "a/" + name; !listed[key] {
				warn(WarnUnlistedFile, key)
			}
		}
	}
	if warnings > MaxWarnings {
		b.Warnings[MaxWarnings-1] = Warning{Code: WarnTooManyWarnings}
	}

	if b.Revision, err = revision(b.Title, b.Articles); err != nil {
		return nil, err
	}
	return b, nil
}

// indexLine is a "-" line of index.txt: an article line of the UI bundle
// builder (its key may still fail the article key form) or, with key "", a
// line the builder skips.
type indexLine struct {
	key     string // the path in the parentheses as written
	title   string // cleaned
	section string // cleaned; "" before the first section
}

// parseIndex reads index.txt as parseIndex of the UI bundle builder
// (panel/web/scripts/build-kb.mjs) does. One leading BOM is dropped; lines
// end at \n or \r\n and are trimmed of JavaScript white space (isJSSpace,
// U+FEFF included). Empty lines and every line starting with "Заголовок:" are
// skipped. A line matching articleLine is an article; another line starting
// with "-" is skipped; any other line names the section of the articles below
// it. The builder files articles before the first section under «Разное»;
// their section here is "".
//
// The builder does not read a title of the base: here it is the rest of the
// first non-empty line when that line starts with "Заголовок:".
func parseIndex(text string) (title string, lines []indexLine) {
	section := ""
	first := true
	for _, line := range strings.Split(strings.TrimPrefix(text, "\uFEFF"), "\n") {
		line = strings.TrimFunc(line, isJSSpace) // also the \r of \r\n
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, titlePrefix) {
			if first {
				title = cleanLine(strings.TrimPrefix(line, titlePrefix))
			}
			first = false
			continue
		}
		first = false
		switch m := articleLine.FindStringSubmatch(line); {
		case m != nil:
			lines = append(lines, indexLine{key: m[2], title: cleanLine(m[1]), section: section})
		case strings.HasPrefix(line, "-"):
			lines = append(lines, indexLine{})
		default:
			section = cleanLine(line)
		}
	}
	return title, lines
}

// isJSSpace reports whether r is JavaScript white space or a line terminator
// (jsSpace), which String.prototype.trim removes.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00A0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// cleanLine is a title or a section as served: control and format characters
// (Unicode Cc and Cf) and U+2028, U+2029 become spaces, runs of white space
// one space, the ends are trimmed, and at most MaxLineLength code points are
// kept (a space at the cut is trimmed as well). Cleaning a cleaned line
// changes nothing.
func cleanLine(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || r == 0x2028 || r == 0x2029
	}), " ")
	n := 0
	for i := range s {
		if n == MaxLineLength {
			return strings.TrimRight(s[:i], " ")
		}
		n++
	}
	return s
}

// jsonStringSize is the length of s written as a JSON string, quotes left
// out, by encoding/json without HTML escaping (the article response): '"',
// '\\', \b, \f, \n, \r and \t take two bytes, other characters below U+0020
// six (\u00xx), U+2028 and U+2029 six (\u2028) instead of three; everything
// else is written as is. s is valid UTF-8.
func jsonStringSize(s []byte) int {
	n := len(s)
	for i, c := range s {
		switch {
		case c == '"', c == '\\', c == '\b', c == '\f', c == '\n', c == '\r', c == '\t':
			n++
		case c < 0x20:
			n += 5
		case c == 0xE2 && i+2 < len(s) && s[i+1] == 0x80 && (s[i+2] == 0xA8 || s[i+2] == 0xA9):
			n += 3
		}
	}
	return n
}

// revision is SHA-256 of the canonical JSON (canonjson) of
// {"articles":[{"key","order","section","sha256","title"}…],"schema","title"}
// with the titles and sections as served. The canonical form does not
// escape HTML: '<', '>' and '&' stay as they are, '"' and '\' are written as
// \" and \\. Served titles and sections hold no control characters, U+2028 or
// U+2029, so no other escape occurs in them; how a response is encoded does
// not change the revision. It changes only with the base, never with the
// build version or the time.
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

// readRegularAt reads a single child of a pinned directory. NOFOLLOW denies
// links atomically; NONBLOCK prevents a swapped-in FIFO from waiting for a
// writer before the opened descriptor's type can be checked.
func readRegularAt(dir *os.File, name string, limit int64) ([]byte, error) {
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") {
		return nil, errNotRegular
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, errNotRegular
		}
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() {
		return nil, errNotRegular
	}
	if opened.Size() > limit {
		return nil, errTooLarge
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
