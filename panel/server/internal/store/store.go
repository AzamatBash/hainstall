package store

import (
	"database/sql"
	"encoding/json"
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
	ID                string        `json:"id"`
	Name              string        `json:"name"`
	URL               string        `json:"url"`
	Token             string        `json:"token,omitempty"`
	Country           string        `json:"country"`
	SortOrder         int           `json:"sort_order"`
	RemnaPanelID      string        `json:"remna_panel_id"`
	ProviderID        string        `json:"provider_id"`
	ProviderAccountID string        `json:"provider_account_id"`
	CreatedAt         time.Time     `json:"created_at"`
	LastSeen          *time.Time    `json:"last_seen,omitempty"`
	Status            NodeStatus    `json:"status"`
	Snapshot          *NodeSnapshot `json:"live,omitempty"`
}

// NodeSnapshot is the last known live metrics for the nodes list (Remnawave-style).
type NodeSnapshot struct {
	Sessions  *int              `json:"sessions,omitempty"`
	CPU       *float64          `json:"cpu,omitempty"`
	LoadAvg   []float64         `json:"load_avg,omitempty"`
	DownBps   *float64          `json:"down_bps,omitempty"`
	UpBps     *float64          `json:"up_bps,omitempty"`
	NetRx     int64             `json:"net_rx_bytes,omitempty"`
	NetTx     int64             `json:"net_tx_bytes,omitempty"`
	Backends  []SnapshotBackend `json:"backends,omitempty"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type SnapshotBackend struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
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
  status TEXT NOT NULL DEFAULT 'unknown',
  country TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0
);
`)
	if err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "country", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "sort_order", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "snapshot", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "remna_panel_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "provider_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("nodes", "provider_account_id", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.backfillSortOrder(); err != nil {
		return err
	}
	_, err = s.db.Exec(`
CREATE TABLE IF NOT EXISTS remna_panels (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  base_url TEXT NOT NULL,
  api_key_enc BLOB,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS backend_remna_links (
  node_id TEXT NOT NULL,
  backend TEXT NOT NULL,
  server_name TEXT NOT NULL,
  remna_panel_id TEXT NOT NULL,
  remna_address TEXT NOT NULL,
  PRIMARY KEY (node_id, backend, server_name)
);
CREATE TABLE IF NOT EXISTS providers (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  favicon_url TEXT NOT NULL DEFAULT '',
  login_url TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS provider_accounts (
  id TEXT PRIMARY KEY,
  provider_id TEXT NOT NULL,
  login TEXT NOT NULL,
  created_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	// Existing DBs: remna_node_name → remna_address (no-op if already renamed / never existed).
	_, _ = s.db.Exec(`ALTER TABLE backend_remna_links RENAME COLUMN remna_node_name TO remna_address`)
	return nil
}

func (s *Store) ensureColumn(table, column, decl string) error {
	rows, err := s.db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

// backfillSortOrder assigns 0..n-1 by created_at DESC when every row is still 0.
func (s *Store) backfillSortOrder() error {
	var total, zeros int
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN sort_order = 0 THEN 1 ELSE 0 END), 0) FROM nodes`).Scan(&total, &zeros); err != nil {
		return err
	}
	if total <= 1 || zeros != total {
		return nil
	}
	rows, err := s.db.Query(`SELECT id FROM nodes ORDER BY created_at DESC`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for i, id := range ids {
		if _, err := s.db.Exec(`UPDATE nodes SET sort_order = ? WHERE id = ?`, i, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListNodes() ([]Node, error) {
	rows, err := s.db.Query(`
SELECT id, name, url, token, country, sort_order, remna_panel_id, provider_id, provider_account_id, created_at, last_seen, status, snapshot
FROM nodes ORDER BY sort_order ASC, created_at DESC`)
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
SELECT id, name, url, token, country, sort_order, remna_panel_id, provider_id, provider_account_id, created_at, last_seen, status, snapshot
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

func (s *Store) nextSortOrder() (int, error) {
	var next int
	err := s.db.QueryRow(`SELECT COALESCE(MAX(sort_order), -1) + 1 FROM nodes`).Scan(&next)
	return next, err
}

func (s *Store) CreateNode(name, url, token string) (*Node, error) {
	now := time.Now().UTC()
	order, err := s.nextSortOrder()
	if err != nil {
		return nil, err
	}
	n := Node{
		ID:        uuid.NewString(),
		Name:      name,
		URL:       url,
		Token:     token,
		Country:   "",
		SortOrder: order,
		CreatedAt: now,
		Status:    StatusUnknown,
	}
	_, err = s.db.Exec(`
INSERT INTO nodes (id, name, url, token, country, sort_order, remna_panel_id, provider_id, provider_account_id, created_at, last_seen, status)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)`,
		n.ID, n.Name, n.URL, n.Token, n.Country, n.SortOrder, n.RemnaPanelID, n.ProviderID, n.ProviderAccountID, n.CreatedAt.Format(time.RFC3339Nano), string(n.Status))
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

// UpdateNodeMeta updates name, country, and/or remna panel without touching URL/status.
// Empty remnaPanelID clears the association.
func (s *Store) UpdateNodeMeta(id string, name *string, country *string, remnaPanelID *string, providerID *string, providerAccountID *string) (*Node, error) {
	n, err := s.GetNode(id)
	if n == nil || err != nil {
		return n, err
	}
	newName := n.Name
	newCountry := n.Country
	newRemna := n.RemnaPanelID
	newProvider := n.ProviderID
	newAccount := n.ProviderAccountID
	if name != nil {
		newName = *name
	}
	if country != nil {
		newCountry = *country
	}
	if remnaPanelID != nil {
		newRemna = *remnaPanelID
	}
	if providerID != nil {
		if *providerID != n.ProviderID && providerAccountID == nil {
			newAccount = ""
		}
		newProvider = *providerID
	}
	if providerAccountID != nil {
		newAccount = *providerAccountID
	}
	_, err = s.db.Exec(`UPDATE nodes SET name = ?, country = ?, remna_panel_id = ?, provider_id = ?, provider_account_id = ? WHERE id = ?`,
		newName, newCountry, newRemna, newProvider, newAccount, id)
	if err != nil {
		return nil, err
	}
	return s.GetNode(id)
}

// ReorderNodes sets sort_order from the given id sequence (0..n-1).
func (s *Store) ReorderNodes(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for i, id := range ids {
		res, err := tx.Exec(`UPDATE nodes SET sort_order = ? WHERE id = ?`, i, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("node not found: %s", id)
		}
	}
	return tx.Commit()
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

// SaveSnapshot stores live metrics and optionally updates status/last_seen.
func (s *Store) SaveSnapshot(id string, status NodeStatus, snap NodeSnapshot) error {
	now := time.Now().UTC()
	snap.UpdatedAt = now
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
UPDATE nodes SET status = ?, last_seen = ?, snapshot = ? WHERE id = ?`,
		string(status), now.Format(time.RFC3339Nano), string(raw), id)
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
		snapshot  sql.NullString
	)
	if err := r.Scan(&n.ID, &n.Name, &n.URL, &n.Token, &n.Country, &n.SortOrder, &n.RemnaPanelID, &n.ProviderID, &n.ProviderAccountID, &createdAt, &lastSeen, &status, &snapshot); err != nil {
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
	if snapshot.Valid && snapshot.String != "" {
		var snap NodeSnapshot
		if err := json.Unmarshal([]byte(snapshot.String), &snap); err == nil {
			n.Snapshot = &snap
		}
	}
	return n, nil
}

// RemnaPanel is a Remnawave panel connection (API key never exposed in JSON).
type RemnaPanel struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	BaseURL   string    `json:"base_url"`
	HasAPIKey bool      `json:"has_api_key"`
	CreatedAt time.Time `json:"created_at"`
	// ApiKeyEnc is the AES-GCM ciphertext; only populated by GetRemnaPanel for server use.
	ApiKeyEnc []byte `json:"-"`
}

// BackendRemnaLink maps a local node backend/server to a Remnawave panel node.
type BackendRemnaLink struct {
	NodeID       string `json:"node_id"`
	Backend      string `json:"backend"`
	ServerName   string `json:"server_name"`
	RemnaPanelID string `json:"remna_panel_id"`
	RemnaAddress string `json:"remna_address"`
}

func (s *Store) ListRemnaPanels() ([]RemnaPanel, error) {
	rows, err := s.db.Query(`
SELECT id, name, base_url, api_key_enc, created_at
FROM remna_panels ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []RemnaPanel
	for rows.Next() {
		p, err := scanRemnaPanel(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []RemnaPanel{}
	}
	return out, rows.Err()
}

// GetRemnaPanel returns a panel including ApiKeyEnc for server-side decrypt.
func (s *Store) GetRemnaPanel(id string) (*RemnaPanel, error) {
	row := s.db.QueryRow(`
SELECT id, name, base_url, api_key_enc, created_at
FROM remna_panels WHERE id = ?`, id)
	p, err := scanRemnaPanel(row, true)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateRemnaPanel(name, baseURL string, apiKeyEnc []byte) (*RemnaPanel, error) {
	now := time.Now().UTC()
	p := RemnaPanel{
		ID:        uuid.NewString(),
		Name:      name,
		BaseURL:   baseURL,
		HasAPIKey: len(apiKeyEnc) > 0,
		CreatedAt: now,
		ApiKeyEnc: apiKeyEnc,
	}
	_, err := s.db.Exec(`
INSERT INTO remna_panels (id, name, base_url, api_key_enc, created_at)
VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.BaseURL, p.ApiKeyEnc, p.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateRemnaPanel updates name/baseURL. If apiKeyEnc is nil the existing key is kept;
// a non-nil pointer (even to empty slice) replaces the stored ciphertext.
func (s *Store) UpdateRemnaPanel(id, name, baseURL string, apiKeyEnc *[]byte) (*RemnaPanel, error) {
	p, err := s.GetRemnaPanel(id)
	if p == nil || err != nil {
		return p, err
	}
	if name == "" {
		name = p.Name
	}
	if baseURL == "" {
		baseURL = p.BaseURL
	}
	enc := p.ApiKeyEnc
	if apiKeyEnc != nil {
		enc = *apiKeyEnc
	}
	_, err = s.db.Exec(`
UPDATE remna_panels SET name = ?, base_url = ?, api_key_enc = ? WHERE id = ?`,
		name, baseURL, enc, id)
	if err != nil {
		return nil, err
	}
	return s.GetRemnaPanel(id)
}

// DeleteRemnaPanel removes the panel and any backend_remna_links that reference it.
func (s *Store) DeleteRemnaPanel(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE nodes SET remna_panel_id = '' WHERE remna_panel_id = ?`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM backend_remna_links WHERE remna_panel_id = ?`, id); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM remna_panels WHERE id = ?`, id)
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

// Provider is a VPS/hosting provider shown as a badge next to the node address.
type Provider struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	FaviconURL string    `json:"favicon_url"`
	LoginURL   string    `json:"login_url"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`
SELECT id, name, favicon_url, login_url, created_at
FROM providers ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if out == nil {
		out = []Provider{}
	}
	return out, rows.Err()
}

func (s *Store) GetProvider(id string) (*Provider, error) {
	row := s.db.QueryRow(`
SELECT id, name, favicon_url, login_url, created_at
FROM providers WHERE id = ?`, id)
	p, err := scanProvider(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) CreateProvider(name, faviconURL, loginURL string) (*Provider, error) {
	now := time.Now().UTC()
	p := Provider{
		ID:         uuid.NewString(),
		Name:       name,
		FaviconURL: faviconURL,
		LoginURL:   loginURL,
		CreatedAt:  now,
	}
	_, err := s.db.Exec(`
INSERT INTO providers (id, name, favicon_url, login_url, created_at)
VALUES (?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.FaviconURL, p.LoginURL, p.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) UpdateProvider(id, name, faviconURL, loginURL string) (*Provider, error) {
	p, err := s.GetProvider(id)
	if p == nil || err != nil {
		return p, err
	}
	if name == "" {
		name = p.Name
	}
	_, err = s.db.Exec(`
UPDATE providers SET name = ?, favicon_url = ?, login_url = ? WHERE id = ?`,
		name, faviconURL, loginURL, id)
	if err != nil {
		return nil, err
	}
	return s.GetProvider(id)
}

func (s *Store) DeleteProvider(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE nodes SET provider_id = '', provider_account_id = '' WHERE provider_id = ?`, id); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM provider_accounts WHERE provider_id = ?`, id); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM providers WHERE id = ?`, id)
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

// ProviderAccount is a login/account under a hosting provider.
type ProviderAccount struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"provider_id"`
	Login      string    `json:"login"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) ListProviderAccounts(providerID string) ([]ProviderAccount, error) {
	rows, err := s.db.Query(`
SELECT id, provider_id, login, created_at
FROM provider_accounts WHERE provider_id = ?
ORDER BY login COLLATE NOCASE ASC`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderAccount
	for rows.Next() {
		a, err := scanProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []ProviderAccount{}
	}
	return out, rows.Err()
}

func (s *Store) ListAllProviderAccounts() ([]ProviderAccount, error) {
	rows, err := s.db.Query(`
SELECT id, provider_id, login, created_at
FROM provider_accounts ORDER BY login COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderAccount
	for rows.Next() {
		a, err := scanProviderAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if out == nil {
		out = []ProviderAccount{}
	}
	return out, rows.Err()
}

func (s *Store) GetProviderAccount(id string) (*ProviderAccount, error) {
	row := s.db.QueryRow(`
SELECT id, provider_id, login, created_at
FROM provider_accounts WHERE id = ?`, id)
	a, err := scanProviderAccount(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) CreateProviderAccount(providerID, login string) (*ProviderAccount, error) {
	now := time.Now().UTC()
	a := ProviderAccount{
		ID:         uuid.NewString(),
		ProviderID: providerID,
		Login:      login,
		CreatedAt:  now,
	}
	_, err := s.db.Exec(`
INSERT INTO provider_accounts (id, provider_id, login, created_at)
VALUES (?, ?, ?, ?)`,
		a.ID, a.ProviderID, a.Login, a.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (s *Store) UpdateProviderAccount(id, login string) (*ProviderAccount, error) {
	a, err := s.GetProviderAccount(id)
	if a == nil || err != nil {
		return a, err
	}
	if login == "" {
		login = a.Login
	}
	_, err = s.db.Exec(`UPDATE provider_accounts SET login = ? WHERE id = ?`, login, id)
	if err != nil {
		return nil, err
	}
	return s.GetProviderAccount(id)
}

func (s *Store) DeleteProviderAccount(id string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE nodes SET provider_account_id = '' WHERE provider_account_id = ?`, id); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM provider_accounts WHERE id = ?`, id)
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

func scanProviderAccount(r rowScanner) (ProviderAccount, error) {
	var (
		a         ProviderAccount
		createdAt string
	)
	if err := r.Scan(&a.ID, &a.ProviderID, &a.Login, &createdAt); err != nil {
		return ProviderAccount{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return ProviderAccount{}, err
		}
	}
	a.CreatedAt = t
	return a, nil
}

func scanProvider(r rowScanner) (Provider, error) {
	var (
		p         Provider
		createdAt string
	)
	if err := r.Scan(&p.ID, &p.Name, &p.FaviconURL, &p.LoginURL, &createdAt); err != nil {
		return Provider{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return Provider{}, err
		}
	}
	p.CreatedAt = t
	return p, nil
}

func (s *Store) GetBackendRemnaLinks(nodeID string) ([]BackendRemnaLink, error) {
	rows, err := s.db.Query(`
SELECT node_id, backend, server_name, remna_panel_id, remna_address
FROM backend_remna_links WHERE node_id = ?
ORDER BY backend ASC, server_name ASC`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BackendRemnaLink
	for rows.Next() {
		var l BackendRemnaLink
		if err := rows.Scan(&l.NodeID, &l.Backend, &l.ServerName, &l.RemnaPanelID, &l.RemnaAddress); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []BackendRemnaLink{}
	}
	return out, rows.Err()
}

// ListAllBackendRemnaLinks returns Remnawave links for every node (for list/search).
func (s *Store) ListAllBackendRemnaLinks() ([]BackendRemnaLink, error) {
	rows, err := s.db.Query(`
SELECT node_id, backend, server_name, remna_panel_id, remna_address
FROM backend_remna_links
ORDER BY node_id ASC, backend ASC, server_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BackendRemnaLink
	for rows.Next() {
		var l BackendRemnaLink
		if err := rows.Scan(&l.NodeID, &l.Backend, &l.ServerName, &l.RemnaPanelID, &l.RemnaAddress); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	if out == nil {
		out = []BackendRemnaLink{}
	}
	return out, rows.Err()
}

func (s *Store) UpsertBackendRemnaLink(nodeID, backend, serverName, remnaPanelID, remnaAddress string) (*BackendRemnaLink, error) {
	_, err := s.db.Exec(`
INSERT INTO backend_remna_links (node_id, backend, server_name, remna_panel_id, remna_address)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(node_id, backend, server_name) DO UPDATE SET
  remna_panel_id = excluded.remna_panel_id,
  remna_address = excluded.remna_address`,
		nodeID, backend, serverName, remnaPanelID, remnaAddress)
	if err != nil {
		return nil, err
	}
	return &BackendRemnaLink{
		NodeID:       nodeID,
		Backend:      backend,
		ServerName:   serverName,
		RemnaPanelID: remnaPanelID,
		RemnaAddress: remnaAddress,
	}, nil
}

func (s *Store) DeleteBackendRemnaLink(nodeID, backend, serverName string) (bool, error) {
	res, err := s.db.Exec(`
DELETE FROM backend_remna_links WHERE node_id = ? AND backend = ? AND server_name = ?`,
		nodeID, backend, serverName)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func scanRemnaPanel(r rowScanner, withEnc bool) (RemnaPanel, error) {
	var (
		p         RemnaPanel
		enc       []byte
		createdAt string
	)
	if err := r.Scan(&p.ID, &p.Name, &p.BaseURL, &enc, &createdAt); err != nil {
		return RemnaPanel{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return RemnaPanel{}, err
		}
	}
	p.CreatedAt = t
	p.HasAPIKey = len(enc) > 0
	if withEnc {
		p.ApiKeyEnc = enc
	}
	return p, nil
}
