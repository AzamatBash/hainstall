package store

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type NodeStatus string

const (
	StatusUnknown NodeStatus = "unknown"
	StatusOnline  NodeStatus = "online"
	StatusOffline NodeStatus = "offline"
)

type Node struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Token     string     `json:"token,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	LastSeen  *time.Time `json:"last_seen,omitempty"`
	Status    NodeStatus `json:"status"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  url TEXT NOT NULL,
  token TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_seen TEXT,
  status TEXT NOT NULL DEFAULT 'unknown'
);
`)
	return err
}

func (s *Store) ListNodes() ([]Node, error) {
	rows, err := s.db.Query(`
SELECT id, name, url, token, created_at, last_seen, status
FROM nodes ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if out == nil {
		out = []Node{}
	}
	return out, rows.Err()
}

func (s *Store) GetNode(id string) (*Node, error) {
	row := s.db.QueryRow(`
SELECT id, name, url, token, created_at, last_seen, status
FROM nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) CreateNode(name, url, token string) (*Node, error) {
	now := time.Now().UTC()
	n := Node{
		ID:        uuid.NewString(),
		Name:      name,
		URL:       url,
		Token:     token,
		CreatedAt: now,
		Status:    StatusUnknown,
	}
	_, err := s.db.Exec(`
INSERT INTO nodes (id, name, url, token, created_at, last_seen, status)
VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		n.ID, n.Name, n.URL, n.Token, n.CreatedAt.Format(time.RFC3339Nano), string(n.Status))
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) DeleteNode(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// UpdateNodeURL changes the management URL (and optionally name) for a node.
// Token is preserved. Status is reset to unknown until the next connect check.
func (s *Store) UpdateNodeURL(id, name, url string) (*Node, error) {
	n, err := s.GetNode(id)
	if n == nil || err != nil {
		return n, err
	}
	if name == "" {
		name = n.Name
	}
	if url == "" {
		url = n.URL
	}
	_, err = s.db.Exec(`
UPDATE nodes SET name = ?, url = ?, status = ?, last_seen = NULL WHERE id = ?`,
		name, url, string(StatusUnknown), id)
	if err != nil {
		return nil, err
	}
	return s.GetNode(id)
}

func (s *Store) UpdateNodeStatus(id string, status NodeStatus, lastSeen *time.Time) error {
	var ls any
	if lastSeen != nil {
		ls = lastSeen.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(`UPDATE nodes SET status = ?, last_seen = ? WHERE id = ?`,
		string(status), ls, id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(r rowScanner) (Node, error) {
	var (
		n         Node
		createdAt string
		lastSeen  sql.NullString
		status    string
	)
	if err := r.Scan(&n.ID, &n.Name, &n.URL, &n.Token, &createdAt, &lastSeen, &status); err != nil {
		return Node{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return Node{}, err
		}
	}
	n.CreatedAt = t
	n.Status = NodeStatus(status)
	if lastSeen.Valid && lastSeen.String != "" {
		ls, err := time.Parse(time.RFC3339Nano, lastSeen.String)
		if err != nil {
			ls, err = time.Parse(time.RFC3339, lastSeen.String)
			if err != nil {
				return Node{}, err
			}
		}
		n.LastSeen = &ls
	}
	return n, nil
}
