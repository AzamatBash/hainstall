package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// APIKeyMeta is a stored key without the secret (shown only once at creation).
type APIKeyMeta struct {
	ID         string
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

// APIKeyCreated includes the plaintext secret — return once, never persist.
type APIKeyCreated struct {
	APIKeyMeta
	Key string
}

const apiKeyPrefix = "hp_"

func HashAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateAPIKeySecret() (raw, prefix string, err error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", "", err
	}
	raw = apiKeyPrefix + hex.EncodeToString(b[:])
	prefix = raw
	if len(prefix) > 11 {
		prefix = prefix[:11] // hp_ + 8 hex
	}
	return raw, prefix, nil
}

func (s *Store) ListAPIKeys() ([]APIKeyMeta, error) {
	rows, err := s.db.Query(`
SELECT id, name, prefix, created_at, last_used_at, revoked_at
FROM api_keys
ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]APIKeyMeta, 0)
	for rows.Next() {
		m, err := scanAPIKeyMeta(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CreateAPIKey(name string) (*APIKeyCreated, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "export"
	}
	if len([]rune(name)) > 64 {
		return nil, fmt.Errorf("имя длиннее 64 символов")
	}
	raw, prefix, err := generateAPIKeySecret()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	hash := HashAPIKey(raw)
	_, err = s.db.Exec(`
INSERT INTO api_keys (id, name, prefix, key_hash, created_at)
VALUES (?, ?, ?, ?, ?)`,
		id, name, prefix, hash, now.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return &APIKeyCreated{
		APIKeyMeta: APIKeyMeta{
			ID:        id,
			Name:      name,
			Prefix:    prefix,
			CreatedAt: now,
		},
		Key: raw,
	}, nil
}

func (s *Store) RevokeAPIKey(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
UPDATE api_keys SET revoked_at = ?
WHERE id = ? AND revoked_at IS NULL`, now, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) DeleteAPIKey(id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	res, err := s.db.Exec(`DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ValidAPIKey reports whether raw matches an active (non-revoked) key.
// On success it updates last_used_at (best effort).
func (s *Store) ValidAPIKey(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	hash := HashAPIKey(raw)
	var id string
	err := s.db.QueryRow(`
SELECT id FROM api_keys
WHERE key_hash = ? AND revoked_at IS NULL`, hash).Scan(&id)
	if err != nil {
		return false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id)
	return true
}

func scanAPIKeyMeta(row rowScanner) (APIKeyMeta, error) {
	var m APIKeyMeta
	var created string
	var lastUsed, revoked sql.NullString
	if err := row.Scan(&m.ID, &m.Name, &m.Prefix, &created, &lastUsed, &revoked); err != nil {
		return m, err
	}
	m.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if lastUsed.Valid && lastUsed.String != "" {
		t, err := time.Parse(time.RFC3339, lastUsed.String)
		if err == nil {
			m.LastUsedAt = &t
		}
	}
	if revoked.Valid && revoked.String != "" {
		t, err := time.Parse(time.RFC3339, revoked.String)
		if err == nil {
			m.RevokedAt = &t
		}
	}
	return m, nil
}
