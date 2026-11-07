package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
)

// OpenExisting opens an already initialised panel database for short operator
// commands (e.g. `panel apikey ...`) that may run next to the live server.
// Unlike Open it never creates the file and never runs migrations or backfills;
// it only checks that the schema already has the api_keys.scopes column.
//
// path must be a plain file path: "file:" URIs, paths with "?" parameters and
// paths with "#" (an SQLite URI fragment that would drop mode=rw) are refused,
// because they would bypass mode=rw (and could create an empty database) and
// the busy timeout. Every connection gets the per-connection
// pragmas of Open (busy_timeout, synchronous, foreign_keys); journal_mode is
// not set, since it persists in the file: the live database is already WAL and
// every connection follows the file's mode.
func OpenExisting(path string) (*Store, error) {
	if strings.HasPrefix(strings.TrimSpace(path), "file:") || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("путь к базе %q не поддерживается: укажите обычный путь к файлу panel.db, без префикса «file:» и символов «?» и «#»", path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("база данных %s не найдена: %w", path, err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s не является файлом базы данных", path)
	}
	db, err := sql.Open("sqlite", existingDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	s := &Store{db: db}
	ok, err := s.hasColumn("api_keys", "scopes")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("проверка схемы: %w", err)
	}
	if !ok {
		_ = db.Close()
		return nil, fmt.Errorf("в базе нет колонки api_keys.scopes: сначала запустите панель новой версии, она обновит схему")
	}
	return s, nil
}

// existingDSN is sqliteDSN for a plain path (OpenExisting refuses the others)
// with mode=rw, so a missing file is an error instead of a new empty database,
// and without journal_mode: WAL stays as set in the file.
func existingDSN(path string) string {
	return "file:" + path + "?mode=rw&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)"
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
