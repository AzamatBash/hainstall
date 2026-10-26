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
func OpenExisting(path string) (*Store, error) {
	if !strings.HasPrefix(path, "file:") {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("база данных %s не найдена: %w", path, err)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s не является файлом базы данных", path)
		}
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

// existingDSN is sqliteDSN without journal_mode and with mode=rw, so a missing
// file is an error instead of a new empty database. WAL stays as set in the file.
func existingDSN(path string) string {
	if strings.HasPrefix(path, "file:") || strings.Contains(path, "?") {
		return path
	}
	return "file:" + path + "?mode=rw&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
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
