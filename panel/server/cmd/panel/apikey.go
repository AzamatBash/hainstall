package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/azabash/hapanel/panel/internal/config"
	"github.com/azabash/hapanel/panel/internal/store"
)

const apiKeyUsage = `использование:
  panel apikey create [--name <имя>] [--scope config] [--db <путь>]
      создать ключ; в stdout печатается только сам ключ (одна строка)
  panel apikey list [--db <путь>]
      показать ключи без секретов: id, имя, префикс, права, даты
По умолчанию база берётся из PANEL_DB_PATH (как у сервера). Путь — обычный путь
к файлу существующей базы, без «file:», «?» и «#». Команда работает рядом с запущенной
панелью и не запускает HTTP-сервер и опросчики.
`

// scopeFlags collects repeated --scope values.
type scopeFlags []string

func (f *scopeFlags) String() string { return strings.Join(*f, ",") }

func (f *scopeFlags) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// runAPIKeyCommand implements `panel apikey ...` and returns the exit code.
func runAPIKeyCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, apiKeyUsage)
		return 2
	}
	switch args[0] {
	case "create":
		return apiKeyCreate(args[1:], stdout, stderr)
	case "list":
		return apiKeyList(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, apiKeyUsage)
		return 0
	default:
		fmt.Fprintf(stderr, "неизвестная команда apikey %q\n", args[0])
		fmt.Fprint(stderr, apiKeyUsage)
		return 2
	}
}

func newAPIKeyFlagSet(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("panel apikey "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, apiKeyUsage) }
	db := fs.String("db", "", "путь к panel.db (по умолчанию PANEL_DB_PATH)")
	return fs, db
}

func apiKeyDBPath(flagValue string) string {
	if p := strings.TrimSpace(flagValue); p != "" {
		return p
	}
	return config.Load().DBPath
}

func apiKeyCreate(args []string, stdout, stderr io.Writer) int {
	fs, db := newAPIKeyFlagSet("create", stderr)
	name := fs.String("name", "", "имя ключа (по умолчанию export)")
	var scopes scopeFlags
	fs.Var(&scopes, "scope", "право ключа (можно повторять): config")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "лишние аргументы: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	normalized, err := store.NormalizeAPIKeyScopes(scopes)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	st, err := store.OpenExisting(apiKeyDBPath(*db))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()
	created, err := st.CreateAPIKeyWithScopes(*name, normalized)
	if err != nil {
		fmt.Fprintln(stderr, "не удалось создать ключ:", err)
		return 1
	}
	fmt.Fprintln(stdout, created.Key)
	fmt.Fprintf(stderr, "ключ создан: id=%s name=%s prefix=%s scopes=%s\n",
		created.ID, cliText(created.Name), created.Prefix, cliScopes(created.Scopes))
	return 0
}

func apiKeyList(args []string, stdout, stderr io.Writer) int {
	fs, db := newAPIKeyFlagSet("list", stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "лишние аргументы: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}
	st, err := store.OpenExisting(apiKeyDBPath(*db))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer st.Close()
	keys, err := st.ListAPIKeys()
	if err != nil {
		fmt.Fprintln(stderr, "не удалось прочитать ключи:", err)
		return 1
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPREFIX\tSCOPES\tCREATED\tLAST_USED\tREVOKED")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			k.ID, cliText(k.Name), k.Prefix, cliScopes(k.Scopes),
			cliTime(&k.CreatedAt), cliTime(k.LastUsedAt), cliTime(k.RevokedAt))
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func cliScopes(scopes []string) string {
	if len(scopes) == 0 {
		return "-"
	}
	return strings.Join(scopes, ",")
}

func cliTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// cliText keeps operator-entered names on one table cell.
func cliText(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
