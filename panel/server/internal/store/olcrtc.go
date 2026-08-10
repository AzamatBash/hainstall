package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Olcrtc node / instance status values.
const (
	OlcrtcStatusUnknown  = "unknown"
	OlcrtcStatusOnline   = "online"
	OlcrtcStatusOffline  = "offline"
	OlcrtcStatusDegraded = "degraded"
)

// OlcrtcNode is a host running olcnode (olcRTC agent). Separate from hanode (HAProxy agent).
// Token is the agent Bearer secret. TODO(secretbox): stop returning raw token in API;
// for local MVP we include it on create/deploy/get so operators can copy it.
type OlcrtcNode struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	AgentURL          string `json:"agent_url"`
	Host              string `json:"host"`
	Country           string `json:"country"`
	ProviderID        string `json:"provider_id"`
	ProviderAccountID string `json:"provider_account_id"`
	Token             string `json:"token,omitempty"`
	HasToken          bool   `json:"has_token"`
	Status            string `json:"status"`
	LastError         string `json:"last_error"`
	LastSeenAt        int64  `json:"last_seen_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// OlcrtcInstance is one tunnel endpoint (provider+transport+room+key).
// key_hex is stored as plaintext hex for local MVP demo; production should use secretbox.
type OlcrtcInstance struct {
	ID        string `json:"id"`
	NodeID    string `json:"node_id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`  // jitsi|telemost|wbstream
	Transport string `json:"transport"` // datachannel|vp8channel|seichannel|videochannel
	RoomID    string `json:"room_id"`
	KeyHex    string `json:"key_hex"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// OlcrtcSub is a public subscription feed of instances.
type OlcrtcSub struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Token     string   `json:"token"`
	Refresh   string   `json:"refresh"`
	Enabled   bool     `json:"enabled"`
	CreatedAt int64    `json:"created_at"`
	Members   []string `json:"instance_ids,omitempty"`
}

// OlcrtcSubMember links a subscription to an instance with optional override comment.
type OlcrtcSubMember struct {
	SubID       string `json:"sub_id"`
	InstanceID  string `json:"instance_id"`
	Sort        int    `json:"sort"`
	NodeComment string `json:"node_comment"`
}

// OlcrtcSubResolved is a subscription with joined instance rows for text rendering.
type OlcrtcSubResolved struct {
	Sub       OlcrtcSub
	Instances []OlcrtcSubInstance
}

// OlcrtcSubInstance is an instance row as it appears in a subscription.
type OlcrtcSubInstance struct {
	OlcrtcInstance
	NodeComment string `json:"node_comment"`
	Sort        int    `json:"sort"`
}

func (s *Store) ListOlcrtcNodes() ([]OlcrtcNode, error) {
	rows, err := s.db.Query(`
SELECT id, name, agent_url, host, country, provider_id, provider_account_id, token, status, last_error, last_seen_at, created_at, updated_at
FROM olcrtc_nodes ORDER BY name COLLATE NOCASE ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OlcrtcNode, 0)
	for rows.Next() {
		n, err := scanOlcrtcNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetOlcrtcNode(id string) (*OlcrtcNode, error) {
	row := s.db.QueryRow(`
SELECT id, name, agent_url, host, country, provider_id, provider_account_id, token, status, last_error, last_seen_at, created_at, updated_at
FROM olcrtc_nodes WHERE id = ?`, id)
	n, err := scanOlcrtcNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// CreateOlcrtcNode inserts a node. token is the agent Bearer secret (may be empty).
func (s *Store) CreateOlcrtcNode(name, agentURL, host, token, country, providerID, providerAccountID string) (*OlcrtcNode, error) {
	now := time.Now().UTC().Unix()
	n := OlcrtcNode{
		ID:                uuid.NewString(),
		Name:              name,
		AgentURL:          agentURL,
		Host:              host,
		Country:           strings.ToUpper(strings.TrimSpace(country)),
		ProviderID:        strings.TrimSpace(providerID),
		ProviderAccountID: strings.TrimSpace(providerAccountID),
		Token:             token,
		HasToken:          token != "",
		Status:            OlcrtcStatusUnknown,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	_, err := s.db.Exec(`
INSERT INTO olcrtc_nodes (id, name, agent_url, host, country, provider_id, provider_account_id, token, status, last_error, last_seen_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', 0, ?, ?)`,
		n.ID, n.Name, n.AgentURL, n.Host, n.Country, n.ProviderID, n.ProviderAccountID, n.Token, n.Status, n.CreatedAt, n.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UpdateOlcrtcNode updates mutable fields. Empty token keeps the previous value.
func (s *Store) UpdateOlcrtcNode(id, name, agentURL, host, token string) (*OlcrtcNode, error) {
	n, err := s.GetOlcrtcNode(id)
	if n == nil || err != nil {
		return n, err
	}
	if name == "" {
		name = n.Name
	}
	if token == "" {
		token = n.Token
	}
	now := time.Now().UTC().Unix()
	_, err = s.db.Exec(`
UPDATE olcrtc_nodes SET name = ?, agent_url = ?, host = ?, token = ?, updated_at = ? WHERE id = ?`,
		name, agentURL, host, token, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetOlcrtcNode(id)
}

// UpdateOlcrtcNodeCountry sets ISO-3166 alpha-2 country (or empty).
func (s *Store) UpdateOlcrtcNodeCountry(id, country string) (*OlcrtcNode, error) {
	n, err := s.GetOlcrtcNode(id)
	if n == nil || err != nil {
		return n, err
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	now := time.Now().UTC().Unix()
	_, err = s.db.Exec(`UPDATE olcrtc_nodes SET country = ?, updated_at = ? WHERE id = ?`, country, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetOlcrtcNode(id)
}

// UpdateOlcrtcNodeProvider sets hosting provider + optional account (hapanel providers).
func (s *Store) UpdateOlcrtcNodeProvider(id, providerID, providerAccountID string) (*OlcrtcNode, error) {
	n, err := s.GetOlcrtcNode(id)
	if n == nil || err != nil {
		return n, err
	}
	providerID = strings.TrimSpace(providerID)
	providerAccountID = strings.TrimSpace(providerAccountID)
	if providerID == "" {
		providerAccountID = ""
	}
	now := time.Now().UTC().Unix()
	_, err = s.db.Exec(
		`UPDATE olcrtc_nodes SET provider_id = ?, provider_account_id = ?, updated_at = ? WHERE id = ?`,
		providerID, providerAccountID, now, id,
	)
	if err != nil {
		return nil, err
	}
	return s.GetOlcrtcNode(id)
}

func (s *Store) DeleteOlcrtcNode(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM olcrtc_nodes WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetOlcrtcNodeStatus updates fake/live status fields for a node.
func (s *Store) SetOlcrtcNodeStatus(id, status, lastError string, lastSeenAt int64) (*OlcrtcNode, error) {
	n, err := s.GetOlcrtcNode(id)
	if n == nil || err != nil {
		return n, err
	}
	now := time.Now().UTC().Unix()
	_, err = s.db.Exec(`
UPDATE olcrtc_nodes SET status = ?, last_error = ?, last_seen_at = ?, updated_at = ? WHERE id = ?`,
		status, lastError, lastSeenAt, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetOlcrtcNode(id)
}

// ListOlcrtcInstances returns instances for a node, or all when nodeID is empty.
func (s *Store) ListOlcrtcInstances(nodeID string) ([]OlcrtcInstance, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if nodeID == "" {
		rows, err = s.db.Query(`
SELECT id, node_id, name, provider, transport, room_id, key_hex, comment, enabled, created_at, updated_at
FROM olcrtc_instances ORDER BY name COLLATE NOCASE ASC`)
	} else {
		rows, err = s.db.Query(`
SELECT id, node_id, name, provider, transport, room_id, key_hex, comment, enabled, created_at, updated_at
FROM olcrtc_instances WHERE node_id = ? ORDER BY name COLLATE NOCASE ASC`, nodeID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OlcrtcInstance, 0)
	for rows.Next() {
		inst, err := scanOlcrtcInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

func (s *Store) GetOlcrtcInstance(id string) (*OlcrtcInstance, error) {
	row := s.db.QueryRow(`
SELECT id, node_id, name, provider, transport, room_id, key_hex, comment, enabled, created_at, updated_at
FROM olcrtc_instances WHERE id = ?`, id)
	inst, err := scanOlcrtcInstance(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (s *Store) CreateOlcrtcInstance(nodeID, name, provider, transport, roomID, keyHex, comment string, enabled bool) (*OlcrtcInstance, error) {
	now := time.Now().UTC().Unix()
	en := 0
	if enabled {
		en = 1
	}
	inst := OlcrtcInstance{
		ID:        uuid.NewString(),
		NodeID:    nodeID,
		Name:      name,
		Provider:  provider,
		Transport: transport,
		RoomID:    roomID,
		KeyHex:    keyHex,
		Comment:   comment,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec(`
INSERT INTO olcrtc_instances (id, node_id, name, provider, transport, room_id, key_hex, comment, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID, inst.NodeID, inst.Name, inst.Provider, inst.Transport, inst.RoomID, inst.KeyHex, inst.Comment, en, inst.CreatedAt, inst.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func (s *Store) UpdateOlcrtcInstance(id, name, provider, transport, roomID, keyHex, comment string, enabled *bool) (*OlcrtcInstance, error) {
	inst, err := s.GetOlcrtcInstance(id)
	if inst == nil || err != nil {
		return inst, err
	}
	if name == "" {
		name = inst.Name
	}
	if provider == "" {
		provider = inst.Provider
	}
	if transport == "" {
		transport = inst.Transport
	}
	if roomID == "" {
		roomID = inst.RoomID
	}
	if keyHex == "" {
		keyHex = inst.KeyHex
	}
	// Empty comment on update keeps the previous value (use a dedicated clear path later if needed).
	if comment == "" {
		comment = inst.Comment
	}
	en := 0
	if enabled != nil {
		if *enabled {
			en = 1
		}
	} else if inst.Enabled {
		en = 1
	}
	now := time.Now().UTC().Unix()
	_, err = s.db.Exec(`
UPDATE olcrtc_instances SET name = ?, provider = ?, transport = ?, room_id = ?, key_hex = ?, comment = ?, enabled = ?, updated_at = ?
WHERE id = ?`,
		name, provider, transport, roomID, keyHex, comment, en, now, id)
	if err != nil {
		return nil, err
	}
	return s.GetOlcrtcInstance(id)
}

func (s *Store) DeleteOlcrtcInstance(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM olcrtc_instances WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ListOlcrtcSubs() ([]OlcrtcSub, error) {
	rows, err := s.db.Query(`
SELECT id, name, token, refresh, enabled, created_at
FROM olcrtc_subs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]OlcrtcSub, 0)
	for rows.Next() {
		sub, err := scanOlcrtcSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		members, err := s.listOlcrtcSubMemberIDs(out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Members = members
	}
	return out, nil
}

func (s *Store) CreateOlcrtcSub(name, refresh string, instanceIDs []string) (*OlcrtcSub, error) {
	if refresh == "" {
		refresh = "10m"
	}
	now := time.Now().UTC().Unix()
	sub := OlcrtcSub{
		ID:        uuid.NewString(),
		Name:      name,
		Token:     uuid.NewString(),
		Refresh:   refresh,
		Enabled:   true,
		CreatedAt: now,
		Members:   append([]string(nil), instanceIDs...),
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT INTO olcrtc_subs (id, name, token, refresh, enabled, created_at)
VALUES (?, ?, ?, ?, 1, ?)`,
		sub.ID, sub.Name, sub.Token, sub.Refresh, sub.CreatedAt); err != nil {
		return nil, err
	}
	if err := setOlcrtcSubMembersTx(tx, sub.ID, instanceIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &sub, nil
}

func (s *Store) DeleteOlcrtcSub(id string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM olcrtc_subs WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) SetOlcrtcSubMembers(subID string, instanceIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := setOlcrtcSubMembersTx(tx, subID, instanceIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func setOlcrtcSubMembersTx(tx *sql.Tx, subID string, instanceIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM olcrtc_sub_members WHERE sub_id = ?`, subID); err != nil {
		return err
	}
	for i, id := range instanceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, err := tx.Exec(`
INSERT INTO olcrtc_sub_members (sub_id, instance_id, sort, node_comment)
VALUES (?, ?, ?, '')`, subID, id, i); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listOlcrtcSubMemberIDs(subID string) ([]string, error) {
	rows, err := s.db.Query(`
SELECT instance_id FROM olcrtc_sub_members WHERE sub_id = ? ORDER BY sort ASC`, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetOlcrtcSubByToken returns a subscription and its enabled instances for public feed rendering.
func (s *Store) GetOlcrtcSubByToken(token string) (*OlcrtcSubResolved, error) {
	row := s.db.QueryRow(`
SELECT id, name, token, refresh, enabled, created_at
FROM olcrtc_subs WHERE token = ?`, token)
	sub, err := scanOlcrtcSub(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
SELECT i.id, i.node_id, i.name, i.provider, i.transport, i.room_id, i.key_hex, i.comment, i.enabled, i.created_at, i.updated_at,
       m.sort, m.node_comment
FROM olcrtc_sub_members m
JOIN olcrtc_instances i ON i.id = m.instance_id
WHERE m.sub_id = ?
ORDER BY m.sort ASC`, sub.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	insts := make([]OlcrtcSubInstance, 0)
	for rows.Next() {
		var (
			si   OlcrtcSubInstance
			en   int
			sort int
		)
		if err := rows.Scan(
			&si.ID, &si.NodeID, &si.Name, &si.Provider, &si.Transport, &si.RoomID, &si.KeyHex, &si.Comment, &en, &si.CreatedAt, &si.UpdatedAt,
			&sort, &si.NodeComment,
		); err != nil {
			return nil, err
		}
		si.Enabled = en != 0
		si.Sort = sort
		insts = append(insts, si)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	members, err := s.listOlcrtcSubMemberIDs(sub.ID)
	if err != nil {
		return nil, err
	}
	sub.Members = members
	return &OlcrtcSubResolved{Sub: sub, Instances: insts}, nil
}

// SeedOlcrtcDemo is a no-op: instances live on olcnode; use POST /api/olcrtc/nodes/deploy-local.
func (s *Store) SeedOlcrtcDemo() (bool, error) {
	return false, nil
}

func scanOlcrtcNode(r rowScanner) (OlcrtcNode, error) {
	var n OlcrtcNode
	if err := r.Scan(
		&n.ID, &n.Name, &n.AgentURL, &n.Host, &n.Country, &n.ProviderID, &n.ProviderAccountID,
		&n.Token, &n.Status, &n.LastError, &n.LastSeenAt, &n.CreatedAt, &n.UpdatedAt,
	); err != nil {
		return OlcrtcNode{}, err
	}
	n.HasToken = n.Token != ""
	return n, nil
}

// OlcrtcNodePublic returns a copy safe for list responses (no raw token).
func OlcrtcNodePublic(n OlcrtcNode) OlcrtcNode {
	n.HasToken = n.Token != ""
	n.Token = ""
	return n
}

func scanOlcrtcInstance(r rowScanner) (OlcrtcInstance, error) {
	var (
		inst OlcrtcInstance
		en   int
	)
	if err := r.Scan(&inst.ID, &inst.NodeID, &inst.Name, &inst.Provider, &inst.Transport, &inst.RoomID, &inst.KeyHex, &inst.Comment, &en, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
		return OlcrtcInstance{}, err
	}
	inst.Enabled = en != 0
	return inst, nil
}

func scanOlcrtcSub(r rowScanner) (OlcrtcSub, error) {
	var (
		sub OlcrtcSub
		en  int
	)
	if err := r.Scan(&sub.ID, &sub.Name, &sub.Token, &sub.Refresh, &en, &sub.CreatedAt); err != nil {
		return OlcrtcSub{}, err
	}
	sub.Enabled = en != 0
	return sub, nil
}

// ValidOlcrtcProvider reports whether provider is supported in MVP.
func ValidOlcrtcProvider(p string) bool {
	switch p {
	case "jitsi", "telemost", "wbstream":
		return true
	default:
		return false
	}
}

// ValidOlcrtcTransport reports whether transport is supported in MVP.
func ValidOlcrtcTransport(t string) bool {
	switch t {
	case "datachannel", "vp8channel", "seichannel", "videochannel":
		return true
	default:
		return false
	}
}

// CompatibleOlcrtcPair reports whether provider+transport is allowed (olcRTC matrix).
func CompatibleOlcrtcPair(provider, transport string) bool {
	provider = strings.TrimSpace(provider)
	transport = strings.TrimSpace(transport)
	switch provider {
	case "jitsi":
		return ValidOlcrtcTransport(transport)
	case "telemost":
		return transport == "vp8channel" || transport == "videochannel"
	case "wbstream":
		return ValidOlcrtcTransport(transport)
	default:
		return false
	}
}

// ValidateOlcrtcKeyHex checks MVP crypto key length (32 bytes = 64 hex).
func ValidateOlcrtcKeyHex(key string) error {
	key = strings.TrimSpace(key)
	if len(key) != 64 {
		return fmt.Errorf("key_hex должен быть 64 hex-символа")
	}
	for _, c := range key {
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !ok {
			return fmt.Errorf("key_hex должен быть hex")
		}
	}
	return nil
}
