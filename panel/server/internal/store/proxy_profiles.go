package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/proxyconfig"
	"github.com/google/uuid"
)

const (
	ProxyEngineHAProxy = "haproxy"
	ProxyEngineEnvoy   = "envoy"

	ProxyApplyIdle    = ""
	ProxyApplyPending = "pending" // assigned in panel; agent apply not wired yet
	ProxyApplyApplied = "applied"
	ProxyApplyError   = "error"
)

// ProxyProfile is a panel-owned proxy config document (HAProxy or Envoy).
// Applying to a live node is a separate step (agent update later).
type ProxyProfile struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Engine    string    `json:"engine"` // haproxy | envoy
	Body      string    `json:"body"`
	Revision  int       `json:"revision"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) migrateProxyProfiles() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS proxy_profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  engine TEXT NOT NULL DEFAULT 'haproxy',
  body TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_proxy_profiles_engine ON proxy_profiles(engine);
`)
	if err != nil {
		return err
	}
	cols := []struct{ name, decl string }{
		{"proxy_engine", "TEXT NOT NULL DEFAULT 'haproxy'"},
		{"proxy_profile_id", "TEXT NOT NULL DEFAULT ''"},
		{"proxy_desired_rev", "INTEGER NOT NULL DEFAULT 0"},
		{"proxy_applied_rev", "INTEGER NOT NULL DEFAULT 0"},
		{"proxy_apply_status", "TEXT NOT NULL DEFAULT ''"},
		{"proxy_apply_error", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, c := range cols {
		if err := s.ensureColumn("nodes", c.name, c.decl); err != nil {
			return err
		}
	}
	return nil
}

func normalizeProxyEngine(engine string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", ProxyEngineHAProxy:
		return ProxyEngineHAProxy, nil
	case ProxyEngineEnvoy:
		return ProxyEngineEnvoy, nil
	default:
		return "", fmt.Errorf("engine: haproxy или envoy")
	}
}

func (s *Store) ListProxyProfiles() ([]ProxyProfile, error) {
	rows, err := s.db.Query(`
SELECT id, name, engine, body, revision, note, created_at, updated_at
FROM proxy_profiles
ORDER BY name COLLATE NOCASE ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProxyProfile, 0)
	for rows.Next() {
		p, err := scanProxyProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProxyProfile(id string) (*ProxyProfile, error) {
	row := s.db.QueryRow(`
SELECT id, name, engine, body, revision, note, created_at, updated_at
FROM proxy_profiles WHERE id = ?`, id)
	p, err := scanProxyProfile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProxyProfile(name, engine, body, note string) (*ProxyProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("укажите название")
	}
	eng, err := normalizeProxyEngine(engine)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	p := ProxyProfile{
		ID:        uuid.NewString(),
		Name:      name,
		Engine:    eng,
		Body:      body,
		Revision:  1,
		Note:      strings.TrimSpace(note),
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err = s.db.Exec(`
INSERT INTO proxy_profiles(id, name, engine, body, revision, note, created_at, updated_at)
VALUES(?,?,?,?,?,?,?,?)`,
		p.ID, p.Name, p.Engine, p.Body, p.Revision, p.Note,
		p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProxyProfile updates metadata/body. Body change bumps revision.
func (s *Store) UpdateProxyProfile(id, name, engine, body, note string, bumpBody bool) (*ProxyProfile, error) {
	cur, err := s.GetProxyProfile(id)
	if err != nil || cur == nil {
		return cur, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = cur.Name
	}
	eng, err := normalizeProxyEngine(engine)
	if err != nil {
		return nil, err
	}
	rev := cur.Revision
	if bumpBody {
		rev++
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`
UPDATE proxy_profiles
SET name=?, engine=?, body=?, revision=?, note=?, updated_at=?
WHERE id=?`,
		name, eng, body, rev, strings.TrimSpace(note), now.Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	return s.GetProxyProfile(id)
}

func (s *Store) DeleteProxyProfile(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Detach nodes in panel only — never touches live agents.
	if _, err := tx.Exec(`
UPDATE nodes SET
  proxy_profile_id='',
  proxy_desired_rev=0,
  proxy_apply_status='',
  proxy_apply_error=''
WHERE proxy_profile_id=?`, id); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM proxy_profiles WHERE id=?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AssignNodeProxyProfile stores desired profile/engine on the node (panel-only).
// Does not contact the agent.
func (s *Store) AssignNodeProxyProfile(nodeID, profileID, engine string) (*Node, error) {
	n, err := s.GetNode(nodeID)
	if n == nil || err != nil {
		return n, err
	}
	profileID = strings.TrimSpace(profileID)
	desiredRev := 0
	status := ProxyApplyIdle
	applyErr := ""
	eng := n.ProxyEngine
	if eng == "" {
		eng = ProxyEngineHAProxy
	}

	if profileID == "" {
		// Clear assignment.
		_, err = s.db.Exec(`
UPDATE nodes SET
  proxy_profile_id='',
  proxy_desired_rev=0,
  proxy_apply_status='',
  proxy_apply_error='',
  proxy_engine=?
WHERE id=?`, eng, nodeID)
		if err != nil {
			return nil, err
		}
		return s.GetNode(nodeID)
	}

	p, err := s.GetProxyProfile(profileID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("профиль не найден")
	}
	if engine != "" {
		eng, err = normalizeProxyEngine(engine)
		if err != nil {
			return nil, err
		}
	} else {
		eng = p.Engine
	}
	if eng != p.Engine {
		return nil, fmt.Errorf("движок ноды (%s) не совпадает с профилем (%s)", eng, p.Engine)
	}
	desiredRev = p.Revision
	status = ProxyApplyPending
	if n.ProxyAppliedRev == p.Revision && n.ProxyProfileID == p.ID {
		status = ProxyApplyApplied
	}
	_, err = s.db.Exec(`
UPDATE nodes SET
  proxy_engine=?,
  proxy_profile_id=?,
  proxy_desired_rev=?,
  proxy_apply_status=?,
  proxy_apply_error=?
WHERE id=?`, eng, p.ID, desiredRev, status, applyErr, nodeID)
	if err != nil {
		return nil, err
	}
	return s.GetNode(nodeID)
}

// SetNodeProxyEngine sets preferred engine without applying config to the agent.
func (s *Store) SetNodeProxyEngine(nodeID, engine string) (*Node, error) {
	n, err := s.GetNode(nodeID)
	if n == nil || err != nil {
		return n, err
	}
	eng, err := normalizeProxyEngine(engine)
	if err != nil {
		return nil, err
	}
	if n.ProxyProfileID != "" {
		p, err := s.GetProxyProfile(n.ProxyProfileID)
		if err != nil {
			return nil, err
		}
		if p != nil && p.Engine != eng {
			return nil, fmt.Errorf("сначала снимите или смените профиль (%s)", p.Engine)
		}
	}
	_, err = s.db.Exec(`UPDATE nodes SET proxy_engine=? WHERE id=?`, eng, nodeID)
	if err != nil {
		return nil, err
	}
	return s.GetNode(nodeID)
}

func scanProxyProfile(r rowScanner) (ProxyProfile, error) {
	var p ProxyProfile
	var created, updated string
	if err := r.Scan(&p.ID, &p.Name, &p.Engine, &p.Body, &p.Revision, &p.Note, &created, &updated); err != nil {
		return ProxyProfile{}, err
	}
	t1, err := parseStoreTime(created)
	if err != nil {
		return ProxyProfile{}, err
	}
	t2, err := parseStoreTime(updated)
	if err != nil {
		return ProxyProfile{}, err
	}
	p.CreatedAt = t1
	p.UpdatedAt = t2
	return p, nil
}

// DefaultHAProxyProfileBody is the standard VPN HAProxy template (agent defaults).
func DefaultHAProxyProfileBody() string {
	return proxyconfig.DefaultHAProxyVPN()
}

// DefaultEnvoyProfileBody is the Envoy starter template.
func DefaultEnvoyProfileBody() string {
	return proxyconfig.DefaultEnvoyVPN()
}
