package kb

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Characters the tests spell by code point.
var (
	chBOM  = string(rune(0xFEFF)) // byte order mark, JavaScript white space, Cf
	chNBSP = string(rune(0x00A0)) // no-break space, Zs
	chIdeo = string(rune(0x3000)) // ideographic space, Zs
	chLS   = string(rune(0x2028)) // line separator, a line terminator in JavaScript
	chPS   = string(rune(0x2029)) // paragraph separator, likewise
	chZWSP = string(rune(0x200B)) // zero width space, Cf, not white space
	chNEL  = string(rune(0x0085)) // next line, Cc, not JavaScript white space
	chRLO  = string(rune(0x202E)) // right-to-left override, Cf
	chEmoj = string(rune(0x1F680))
)

// uiLine is an article line as the UI bundle builder reads it: the path and
// the title and section cleaned, so that it compares with parseIndex.
type uiLine struct {
	Key, Title, Section string
}

// articleLines are the article lines parseIndex found, skipped "-" lines left
// out (the UI builder does not report them).
func articleLines(text string) []uiLine {
	_, lines := parseIndex(text)
	out := []uiLine{}
	for _, l := range lines {
		if l.key != "" {
			out = append(out, uiLine{l.key, l.title, l.section})
		}
	}
	return out
}

// uiBuilderParse runs parseIndex of panel/web/scripts/build-kb.mjs with node
// on each index text and returns its article lines, the raw title and section
// cleaned with cleanLine. Articles before the first section, which the
// builder files under «Разное», get the section "" (the inputs do not use
// «Разное» as a section name). Without node or the builder it returns nil and
// why.
func uiBuilderParse(t *testing.T, texts []string) ([][]uiLine, string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, "node is not installed: no comparison with the UI bundle builder"
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "scripts", "build-kb.mjs"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "panel/web/scripts/build-kb.mjs is not here: no comparison with the UI bundle builder"
	} else if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(src, []byte("function parseIndex(raw) {"))
	if start < 0 {
		t.Fatal("parseIndex is not in build-kb.mjs: compare the loader with the new UI builder")
	}
	end := bytes.Index(src[start:], []byte("\n}\n"))
	if end < 0 {
		t.Fatal("end of parseIndex not found in build-kb.mjs")
	}
	script := string(src[start:start+end+2]) + `
const texts = JSON.parse(require('fs').readFileSync(0, 'utf8'))
process.stdout.write(JSON.stringify(texts.map(parseIndex)))
`
	input, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "-e", script)
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var parsed [][]struct {
		Title    string `json:"title"`
		Articles []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
		} `json:"articles"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("node output: %v", err)
	}
	if len(parsed) != len(texts) {
		t.Fatalf("node parsed %d texts of %d", len(parsed), len(texts))
	}
	result := make([][]uiLine, len(texts))
	for i, sections := range parsed {
		result[i] = []uiLine{}
		for _, s := range sections {
			section := cleanLine(s.Title)
			if s.Title == "Разное" {
				section = ""
			}
			for _, a := range s.Articles {
				result[i] = append(result[i], uiLine{a.Path, cleanLine(a.Title), section})
			}
		}
	}
	return result, ""
}

// randomIndex builds an index.txt from pieces that the JavaScript and Go
// readings of white space, line ends, titles and keys could treat apart.
func randomIndex(r *rand.Rand) string {
	pick := func(from []string) string { return from[r.Intn(len(from))] }
	pieces := []string{
		"-", "-", "- ", " ", "  ", "\t", "\r", "\v", "\f", "\x00", "\x1b", chNBSP, chIdeo, chLS, chPS,
		chBOM, chZWSP, chNEL, chRLO, "(", ")", "((", "a/", "b/", "x", "A", ".txt", ".", "..", "a/x.txt",
		"a/y.txt", "a/X.txt", "a/x y.txt", "(a/x.txt)", " (a/y.txt)", "(a/z.txt) ", "Заголовок:",
		"Заголовок: ", "Раздел", "Т", "статья", chEmoj,
	}
	random := func(n int) string {
		var s strings.Builder
		for ; n > 0; n-- {
			s.WriteString(pick(pieces))
		}
		return s.String()
	}
	// Pieces of an article line "- <title> (<key>)", most of them valid.
	lead := []string{"", "", "", " ", "\t", chBOM, chNBSP, chZWSP, "\x00"}
	dash := []string{"-", "-", "-", "--", "", "*"}
	space := []string{" ", " ", " ", "  ", "\t", chNBSP, chIdeo, "\v", "\f", chBOM, chLS, chPS, "\r", chZWSP, chNEL, ""}
	keys := []string{
		"a/x.txt", "a/y.txt", "a/z.txt", "a/x.txt", "a/x..y.txt", "a/X.txt", " a/x.txt", "a/x y.txt",
		"a/(x.txt", "a/x).txt", "b/x.txt", "a/x.md", "a/.txt", "a/x.txt" + chLS, "a/" + chEmoj + ".txt",
	}
	tail := []string{"", "", "", " ", "\t", chNBSP, chBOM, ")", " (a/w.txt)", "\r", chLS, chZWSP, "x"}

	var text strings.Builder
	if r.Intn(4) == 0 {
		text.WriteString(chBOM)
	}
	for line := r.Intn(10); line >= 0; line-- {
		switch r.Intn(6) {
		case 0, 1, 2:
			text.WriteString(pick(lead) + pick(dash) + pick(space) + random(1+r.Intn(3)) + pick(space) +
				"(" + pick(keys) + ")" + pick(tail))
		case 3:
			text.WriteString(pick(lead) + pick([]string{"Раздел", "Заголовок:", "Заголовок: База", "-", ""}) + random(r.Intn(3)))
		default:
			text.WriteString(random(1 + r.Intn(6)))
		}
		text.WriteString(pick([]string{"\n", "\n", "\r\n", "\r\r\n"}))
	}
	return text.String()
}

// The loader reads index.txt as the UI bundle builder does: the same article
// lines with the same titles and sections, for the cases of
// TestLoad_IndexLikeUIBuilder and the real-looking base in testdata as well as
// for thousands of random lines. Needs node.
func TestParseIndex_MatchesUIBuilder(t *testing.T) {
	testdata, err := os.ReadFile(filepath.Join(testdataBase, "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	texts := []string{string(testdata)}
	for _, tc := range uiBuilderCases {
		texts = append(texts, tc.index)
	}
	r := rand.New(rand.NewSource(20261109))
	for i := 0; i < 3000; i++ {
		texts = append(texts, randomIndex(r))
	}
	ui, skip := uiBuilderParse(t, texts)
	if ui == nil {
		t.Skip(skip)
	}
	articles := 0
	for i, text := range texts {
		got := articleLines(text)
		if !reflect.DeepEqual(got, ui[i]) {
			t.Fatalf("index %q:\nloader %q\nUI     %q", text, got, ui[i])
		}
		articles += len(got)
	}
	if articles < 1000 {
		t.Fatalf("only %d article lines in the comparison: the random index texts are too poor", articles)
	}
	t.Logf("%d index texts, %d article lines read alike", len(texts), articles)
}

type uiBuilderCase struct {
	name     string
	index    string
	files    []string // article files to create (a/<name>.txt), text "<name>"
	title    string
	articles []string // "key|title|section"
	warnings []Warning
}

// Cases the UI bundle builder reads in its own way; the expected articles are
// what parseIndex of build-kb.mjs lists (TestParseIndex_MatchesUIBuilder runs
// it on these inputs), less what fails the checks of the loader.
var uiBuilderCases = []uiBuilderCase{
	{
		name:     "BOM before an article line in the middle of the file",
		index:    "Заголовок: База\nРаздел\n" + chBOM + "- С BOM (a/b.txt)\n- Обычная (a/c.txt)\n- A (a/a.txt)\n",
		files:    []string{"a", "b", "c"},
		title:    "База",
		articles: []string{"a/b.txt|С BOM|Раздел", "a/c.txt|Обычная|Раздел", "a/a.txt|A|Раздел"},
	},
	{
		name:     "later Заголовок: lines are skipped, not sections",
		index:    "Заголовок: База\nРаздел\n- A (a/a.txt)\nЗаголовок: Не раздел\n- B (a/b.txt)\n  Заголовок:\n- C (a/c.txt)\n",
		files:    []string{"a", "b", "c"},
		title:    "База",
		articles: []string{"a/a.txt|A|Раздел", "a/b.txt|B|Раздел", "a/c.txt|C|Раздел"},
	},
	{
		name:     "articles before the first section, no title line",
		index:    "- A (a/a.txt)\nРаздел\n- B (a/b.txt)\nЗаголовок: не первая строка\n",
		files:    []string{"a", "b"},
		articles: []string{"a/a.txt|A|", "a/b.txt|B|Раздел"},
	},
	{
		name:  "the title of the base is cleaned",
		index: "Заголовок:\tБаза\x1bс\x00управляющими" + chLS + "символами " + chZWSP + "\n- A (a/a.txt)\n",
		files: []string{"a"},
		title: "База с управляющими символами", articles: []string{"a/a.txt|A|"},
	},
	{
		name: "titles empty once cleaned get the file name",
		index: "Раздел\n" +
			"-   (a/three.txt)\n" + // the title is one space
			"-  (a/two.txt)\n" + // no title: not an article line
			"- \v (a/vt.txt)\n" + // the title is a vertical tab
			"- " + chZWSP + " (a/zw.txt)\n" +
			"- \x1b[1m (a/esc.txt)\n",
		files:    []string{"three", "two", "vt", "zw", "esc"},
		articles: []string{"a/three.txt|three|Раздел", "a/vt.txt|vt|Раздел", "a/zw.txt|zw|Раздел", "a/esc.txt|[1m|Раздел"},
		warnings: []Warning{
			{WarnEmptyTitle, "a/three.txt"}, {WarnInvalidKey, ""}, {WarnEmptyTitle, "a/vt.txt"}, {WarnEmptyTitle, "a/zw.txt"},
			{WarnUnlistedFile, "a/two.txt"},
		},
	},
	{
		name: "control and format characters in titles and sections",
		index: "Раздел\rс CR внутри\n" +
			"- Заголовок\x1bс ESC\x7fи DEL (a/a.txt)\n" +
			"Раздел\x00с NUL" + chRLO + "\n" +
			"- Тот\x0bсамый\x0cзаголовок\x01 (a/b.txt)\n" +
			"- Перевод" + chLS + "строки (a/c.txt)\n" + // "." of JavaScript does not take U+2028
			"- Разрыв" + chPS + " (a/d.txt)\n" + // U+2029 is white space before the key
			"- Возврат\rкаретки (a/e.txt)\n" + // nor \r
			"- " + chNEL + "NEL" + chNEL + " (a/f.txt)\n",
		files: []string{"a", "b", "c", "d", "e", "f"},
		articles: []string{
			"a/a.txt|Заголовок с ESC и DEL|Раздел с CR внутри", "a/b.txt|Тот самый заголовок|Раздел с NUL",
			"a/d.txt|Разрыв|Раздел с NUL", "a/f.txt|NEL|Раздел с NUL",
		},
		warnings: []Warning{{WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnUnlistedFile, "a/c.txt"}, {WarnUnlistedFile, "a/e.txt"}},
	},
	{
		name: "keys",
		index: "- Две точки в имени (a/xray-1..8.txt)\n" +
			"- Длинный путь (a/" + strings.Repeat("x", 1200) + ".txt)\n" +
			"- Длинная скобка (" + strings.Repeat("x", 1200) + ")\n" +
			"- Пробелы в скобках ( a/sp.txt )\n" +
			"- Другой каталог (b/x.txt)\n" +
			"- Заглавные (a/Upper.txt)\n" +
			"- Две скобки (a/one.txt) (a/two.txt)\n" +
			"- Внутри (a/x (a/y.txt)\n" +
			"- Скобки (в заголовке) (a/paren.txt)\n" +
			"- Повтор (a/two.txt)\n",
		files:    []string{"xray-1..8", "sp", "Upper", "one", "two", "y", "paren", "Bad_Name"},
		articles: []string{"a/xray-1..8.txt|Две точки в имени|", "a/two.txt|Две скобки (a/one.txt)|", "a/paren.txt|Скобки (в заголовке)|"},
		warnings: []Warning{
			{WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnInvalidKey, ""},
			{WarnInvalidKey, ""}, {WarnDuplicateKey, "a/two.txt"},
			{WarnUnlistedFile, ""}, {WarnUnlistedFile, "a/one.txt"}, {WarnUnlistedFile, "a/sp.txt"}, {WarnUnlistedFile, "a/y.txt"},
		},
	},
	{
		name: "white space",
		index: chNBSP + "\n" + chBOM + chBOM + "\n" + // empty lines
			"-\tTab (a/tab.txt)\n" +
			"-" + chNBSP + "Nbsp" + chNBSP + "(a/nbsp.txt)" + chNBSP + "\n" +
			"-" + chIdeo + "Ideographic" + chIdeo + "(a/ideo.txt)\n" +
			"- Без пробела(a/nospace.txt)\n" +
			"- Двойные   пробелы" + chNBSP + chNBSP + "внутри (a/double.txt)\n" +
			chZWSP + "\n" + // a section, empty once cleaned
			"- После ZWSP (a/zwsp.txt)\n" +
			"---\n-\n",
		files: []string{"tab", "nbsp", "ideo", "nospace", "double", "zwsp"},
		articles: []string{
			"a/tab.txt|Tab|", "a/nbsp.txt|Nbsp|", "a/ideo.txt|Ideographic|", "a/double.txt|Двойные пробелы внутри|",
			"a/zwsp.txt|После ZWSP|",
		},
		warnings: []Warning{{WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnInvalidKey, ""}, {WarnUnlistedFile, "a/nospace.txt"}},
	},
	{
		name:  "only CR line ends: one line, the title",
		index: "Заголовок: X\r- A (a/a.txt)\r- B (a/b.txt)\r",
		files: []string{"a", "b"},
		title: "X - A (a/a.txt) - B (a/b.txt)",
		warnings: []Warning{
			{WarnUnlistedFile, "a/a.txt"}, {WarnUnlistedFile, "a/b.txt"},
		},
	},
	{
		name: "long titles and sections are cut at 300 code points",
		index: strings.Repeat("б", 299) + " " + strings.Repeat("в", 100) + "\n" +
			"- " + strings.Repeat("я", 350) + " (a/long.txt)\n",
		files:    []string{"long"},
		articles: []string{"a/long.txt|" + strings.Repeat("я", 300) + "|" + strings.Repeat("б", 299)},
	},
}

func TestLoad_IndexLikeUIBuilder(t *testing.T) {
	for _, tc := range uiBuilderCases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			for _, name := range tc.files {
				files["a/"+name+".txt"] = name
			}
			b := mustLoad(t, writeBase(t, tc.index, files))
			if b.Title != tc.title {
				t.Fatalf("title=%q want %q", b.Title, tc.title)
			}
			got := []string{}
			for i, a := range b.Articles {
				if a.Order != i+1 {
					t.Fatalf("%s order=%d", a.Key, a.Order)
				}
				got = append(got, a.Key+"|"+a.Title+"|"+a.Section)
			}
			want := tc.articles
			if want == nil {
				want = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("articles=%q\nwant %q", got, want)
			}
			wantWarnings := tc.warnings
			if wantWarnings == nil {
				wantWarnings = []Warning{}
			}
			if !reflect.DeepEqual(b.Warnings, wantWarnings) {
				t.Fatalf("warnings=%+v\nwant %+v", b.Warnings, wantWarnings)
			}
		})
	}
}

func TestCleanLine(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"  обычный   текст  ":              "обычный текст",
		"\tтаб\tи\x0bVT\x0cFF\r\nCRLF\x00": "таб и VT FF CRLF",
		"ESC\x1b[0m DEL\x7f C1" + chNEL:    "ESC [0m DEL C1",
		"разрывы" + chLS + "строк" + chPS:  "разрывы строк",
		chBOM + chZWSP + "формат" + chRLO:  "формат",
		chNBSP + "неразрывные" + chIdeo:    "неразрывные",
		"<b>HTML</b> & \"кавычки\" \\":     "<b>HTML</b> & \"кавычки\" \\",
		strings.Repeat("я", 301):           strings.Repeat("я", 300),
		strings.Repeat(chEmoj, 300) + "!":  strings.Repeat(chEmoj, 300),
		strings.Repeat("я", 299) + " ж":    strings.Repeat("я", 299), // the space at the cut goes too
		strings.Repeat("я", 299) + "\x01ж": strings.Repeat("я", 299),
		strings.Repeat(" я", 400):          strings.TrimSpace(strings.Repeat(" я", 150)),
	}
	for in, want := range cases {
		got := cleanLine(in)
		if got != want {
			t.Errorf("cleanLine(%q)=%q want %q", in, got, want)
		}
		if again := cleanLine(got); again != got {
			t.Errorf("cleanLine is not idempotent on %q: %q", got, again)
		}
		if n := utf8.RuneCountInString(got); n > MaxLineLength {
			t.Errorf("cleanLine(%q) has %d code points", in, n)
		}
	}
}

// jsonStringSize follows encoding/json without HTML escaping byte for byte.
func TestJSONStringSize(t *testing.T) {
	texts := []string{"", "обычный текст", "<b>HTML</b> & \"кавычки\" \\", chLS + chPS + chEmoj, "\u00e9\x7f"}
	for c := 0; c < utf8.RuneSelf; c++ {
		texts = append(texts, string(rune(c)), "x"+string(rune(c))+"y")
	}
	for _, s := range texts {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(s); err != nil {
			t.Fatal(err)
		}
		if want := buf.Len() - len("\"\"\n"); jsonStringSize([]byte(s)) != want {
			t.Errorf("jsonStringSize(%q)=%d, encoding/json writes %d (%s)", s, jsonStringSize([]byte(s)), want, buf.Bytes())
		}
	}
}

func TestLoad_ArticleJSONSize(t *testing.T) {
	newlines := strings.Repeat("\n", MaxArticleSize) // \n twice as long in JSON: exactly MaxArticleJSONSize
	controls := "Заголовок: x\n" + strings.Repeat("\x01", MaxArticleJSONSize/6+1)
	b := mustLoad(t, writeBase(t, "- Переводы строк (a/newlines.txt)\n- Управляющие символы (a/controls.txt)\n",
		map[string]string{"a/newlines.txt": newlines, "a/controls.txt": controls}))
	if len(b.Articles) != 1 || b.Articles[0].Key != "a/newlines.txt" || b.Articles[0].Size != MaxArticleSize {
		t.Fatalf("articles=%d", len(b.Articles))
	}
	if want := []Warning{{WarnArticleTooLarge, "a/controls.txt"}}; !reflect.DeepEqual(b.Warnings, want) {
		t.Fatalf("warnings=%+v want %+v", b.Warnings, want)
	}
}

func TestLoad_WarningLimit(t *testing.T) {
	load := func(t *testing.T, bad int) *Base {
		t.Helper()
		index := "Заголовок: Предупреждения\n- Статья (a/ok.txt)\n" + strings.Repeat("- без ключа\n", bad)
		return mustLoad(t, writeBase(t, index, map[string]string{"a/ok.txt": "ok", "a/zz-unlisted.txt": "x"}))
	}
	// Exactly MaxWarnings: all of them, the unlisted file last.
	b := load(t, MaxWarnings-1)
	if len(b.Warnings) != MaxWarnings || b.Warnings[MaxWarnings-1] != (Warning{WarnUnlistedFile, "a/zz-unlisted.txt"}) {
		t.Fatalf("warnings=%d last=%+v", len(b.Warnings), b.Warnings[len(b.Warnings)-1])
	}
	// More: the first MaxWarnings-1, then too_many_warnings without a key.
	for _, bad := range []int{MaxWarnings, 5000} {
		b := load(t, bad)
		if len(b.Warnings) != MaxWarnings {
			t.Fatalf("%d bad lines: %d warnings", bad, len(b.Warnings))
		}
		for _, w := range b.Warnings[:MaxWarnings-1] {
			if w != (Warning{WarnInvalidKey, ""}) {
				t.Fatalf("%d bad lines: warning %+v", bad, w)
			}
		}
		if last := b.Warnings[MaxWarnings-1]; last != (Warning{WarnTooManyWarnings, ""}) {
			t.Fatalf("%d bad lines: last warning %+v", bad, last)
		}
		if len(b.Articles) != 1 || b.Articles[0].Key != "a/ok.txt" {
			t.Fatalf("articles=%+v", b.Articles)
		}
	}
}

// A warning shows a key only when it has the article key form.
func TestLoad_WarningKeysHaveTheKeyForm(t *testing.T) {
	long := "a/" + strings.Repeat("x", 152) + ".txt"
	index := "- Длинный (" + long + ")\n- Пропущен (a/missing.txt)\n- Скобки (a/x (1).txt)\n"
	b := mustLoad(t, writeBase(t, index, map[string]string{
		"a/" + strings.Repeat("y", 200) + ".txt": "y", "a/README.md": "r", "a/.hidden.txt": "h", "a/ok-name.txt": "o",
	}))
	want := []Warning{
		{WarnInvalidKey, ""}, {WarnListedButMissing, "a/missing.txt"}, {WarnInvalidKey, ""},
		{WarnUnlistedFile, ""}, {WarnUnlistedFile, ""}, {WarnUnlistedFile, "a/ok-name.txt"}, {WarnUnlistedFile, ""},
	}
	if !reflect.DeepEqual(b.Warnings, want) {
		t.Fatalf("warnings=%+v\nwant %+v", b.Warnings, want)
	}
	for _, w := range b.Warnings {
		if w.Key != "" && !ValidKey(w.Key) {
			t.Fatalf("warning key %q", w.Key)
		}
	}
	if len(b.Articles) != 0 {
		t.Fatalf("articles=%+v", b.Articles)
	}
}

// Warning codes have the form Panel VPN and the VPN expert accept.
func TestWarningCodes(t *testing.T) {
	code := regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	for _, c := range []string{
		WarnInvalidKey, WarnDuplicateKey, WarnListedButMissing, WarnUnlistedFile, WarnArticleTooLarge,
		WarnInvalidUTF8, WarnTooManyArticles, WarnEmptyTitle, WarnTooManyWarnings,
	} {
		if !code.MatchString(c) {
			t.Errorf("warning code %q", c)
		}
	}
}

// The canonical form of the revision does not escape HTML: '<', '>' and '&'
// as they are, '"' and '\' as \" and \\, titles and sections as served.
func TestLoad_RevisionCanonicalFormKeepsHTML(t *testing.T) {
	index := "Заголовок: <База> & \"кавычки\" \\\n" +
		"Раздел <1>\x1b & 2\n" +
		"- <b>Статья</b> & \"кавычки\"" + chZWSP + " \\ (a/a.txt)\n"
	b := mustLoad(t, writeBase(t, index, map[string]string{"a/a.txt": "<p>текст</p>"}))
	canonical := `{"articles":[{"key":"a/a.txt","order":1,"section":"Раздел <1> & 2","sha256":"` + sha256Hex([]byte("<p>текст</p>")) +
		`","title":"<b>Статья</b> & \"кавычки\" \\"}],"schema":"hapanel.kb.v1","title":"<База> & \"кавычки\" \\"}`
	if b.Revision != sha256Hex([]byte(canonical)) {
		t.Fatalf("revision=%s want sha256 of %s", b.Revision, canonical)
	}
}
