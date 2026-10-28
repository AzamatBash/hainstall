package store

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/protect"
)

// Secret-free configuration rows for the read-only GET /api/v1/config export.
// Each List*Configs method is one SELECT over a small configuration table and
// never reads node tokens, Remnawave API keys/base URLs or provider logins.

// NodeConfig is one hanode's configuration as the panel shows it.
type NodeConfig struct {
	ID                string
	Name              string
	Country           string
	Host              string // hostname/IP from the management URL; "" if unparsable
	SortOrder         int
	CreatedAt         time.Time
	ListenPorts       []int
	Entrances         []Entrance
	Protect           protect.Profile // effective profile (default when unset)
	ProtectDefault    bool            // stored column was empty, default profile applies
	RemnaPanelID      string
	ProviderID        string
	ProviderAccountID string
	TrafficLog        bool
	HasToken          bool
	// Backends are the servers from the poller's saved snapshot (nil without one).
	Backends           []SnapshotBackend
	BackendsObservedAt *time.Time
}

// RemnaPanelConfig is a Remnawave panel without its base URL or API key.
type RemnaPanelConfig struct {
	ID        string
	Name      string
	HasAPIKey bool
	CreatedAt time.Time
}

// RemnaNodeConfig is a catalog row without poller state (online, last seen, …).
type RemnaNodeConfig struct {
	PanelID            string
	RemnaUUID          string
	Name               string
	Address            string
	ConfigProfileUUID  string
	InboundUUIDs       []string
	InboundTags        []string
	Protocol           string // effective: override || derived
	ProtocolDerived    string
	ProtocolOverride   string
	RoleRNFront        bool
	RoleRNBack         bool
	RoleHPFront        bool
	RoleHPBack         bool
	RoleCDNBack        bool
	EnabledInAnalytics bool
	Notes              string
	AlertNote          string
}

// ProviderConfig is a hosting provider without login/favicon URLs.
type ProviderConfig struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

// ProviderAccountConfig is a provider account without its login.
type ProviderAccountConfig struct {
	ID         string
	ProviderID string
	CreatedAt  time.Time
}

// nodeConfigSelect mirrors nodeSelectCols (same order, same scanNode), except
// that the agent token is reduced to a presence flag inside SQLite, and the raw
// protect column is appended once more to tell "unset" from explicit values.
const nodeConfigSelect = `
SELECT id, name, url, CASE WHEN token <> '' THEN '1' ELSE '' END, country, sort_order,
  remna_panel_id, provider_id, provider_account_id, created_at, last_seen, status,
  snapshot, traffic_log, listen_ports, entrances, protect, protect
FROM nodes ORDER BY id ASC`

// scanAppend feeds extra destinations after the ones scanNode asks for.
type scanAppend struct {
	r     rowScanner
	extra []any
}

func (a scanAppend) Scan(dest ...any) error {
	return a.r.Scan(append(dest, a.extra...)...)
}

// ListNodeConfigs returns every hanode's configuration, sorted by id.
func (s *Store) ListNodeConfigs() ([]NodeConfig, error) {
	rows, err := s.db.Query(nodeConfigSelect)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]NodeConfig, 0, 32)
	for rows.Next() {
		var protectRaw sql.NullString
		n, err := scanNode(scanAppend{r: rows, extra: []any{&protectRaw}})
		if err != nil {
			return nil, err
		}
		c := NodeConfig{
			ID:                n.ID,
			Name:              n.Name,
			Country:           n.Country,
			Host:              hostFromNodeURL(n.URL),
			SortOrder:         n.SortOrder,
			CreatedAt:         n.CreatedAt,
			ListenPorts:       n.ListenPorts,
			Entrances:         n.Entrances,
			Protect:           n.Protect,
			ProtectDefault:    !protectRaw.Valid || strings.TrimSpace(protectRaw.String) == "",
			RemnaPanelID:      n.RemnaPanelID,
			ProviderID:        n.ProviderID,
			ProviderAccountID: n.ProviderAccountID,
			TrafficLog:        n.TrafficLog,
			HasToken:          n.Token != "",
		}
		if n.Snapshot != nil {
			c.Backends = n.Snapshot.Backends
			if !n.Snapshot.UpdatedAt.IsZero() {
				at := n.Snapshot.UpdatedAt
				c.BackendsObservedAt = &at
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// hostFromNodeURL returns only the hostname or IP of a management URL
// (no scheme, userinfo, port or path), or "" when it cannot be parsed.
func hostFromNodeURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// ListRemnaPanelConfigs returns Remnawave panels sorted by id. The ciphertext
// is never read: only whether a key is stored.
func (s *Store) ListRemnaPanelConfigs() ([]RemnaPanelConfig, error) {
	rows, err := s.db.Query(`
SELECT id, name, COALESCE(LENGTH(api_key_enc), 0) > 0, created_at
FROM remna_panels ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaPanelConfig, 0, 8)
	for rows.Next() {
		var p RemnaPanelConfig
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.HasAPIKey, &created); err != nil {
			return nil, err
		}
		if p.CreatedAt, err = parseStoredTime(created); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRemnaNodeConfigs returns the Remnawave node catalog sorted by panel and UUID.
func (s *Store) ListRemnaNodeConfigs() ([]RemnaNodeConfig, error) {
	rows, err := s.db.Query(`
SELECT panel_id, remna_uuid, name, address, config_profile_uuid,
  inbound_uuids_json, inbound_tags, protocol_derived, protocol_override,
  role_rn_front, role_rn_back, role_hp_front, role_hp_back, role_cdn_back,
  enabled_in_analytics, notes, alert_note
FROM remna_node_catalog ORDER BY panel_id ASC, remna_uuid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaNodeConfig, 0, 128)
	for rows.Next() {
		var n RemnaNodeConfig
		var uuidsJSON, tags string
		var roleF, roleB, roleHPF, roleHPB, roleCDN, enabled int
		if err := rows.Scan(
			&n.PanelID, &n.RemnaUUID, &n.Name, &n.Address, &n.ConfigProfileUUID,
			&uuidsJSON, &tags, &n.ProtocolDerived, &n.ProtocolOverride,
			&roleF, &roleB, &roleHPF, &roleHPB, &roleCDN,
			&enabled, &n.Notes, &n.AlertNote,
		); err != nil {
			return nil, err
		}
		n.InboundUUIDs = decodeInboundUUIDs(uuidsJSON)
		n.InboundTags = splitInboundTags(tags)
		n.Protocol = effectiveProtocol(n.ProtocolOverride, n.ProtocolDerived)
		n.RoleRNFront = roleF != 0
		n.RoleRNBack = roleB != 0
		n.RoleHPFront = roleHPF != 0
		n.RoleHPBack = roleHPB != 0
		n.RoleCDNBack = roleCDN != 0
		n.EnabledInAnalytics = enabled != 0
		out = append(out, n)
	}
	return out, rows.Err()
}

func decodeInboundUUIDs(raw string) []string {
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil || ids == nil {
		return []string{}
	}
	return ids
}

// splitInboundTags reverses the ", " join UpsertRemnaNodeCatalogSync stores.
func splitInboundTags(raw string) []string {
	out := []string{}
	for _, t := range strings.Split(raw, ", ") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ListProviderConfigs returns hosting providers sorted by id.
func (s *Store) ListProviderConfigs() ([]ProviderConfig, error) {
	rows, err := s.db.Query(`
SELECT id, name, created_at FROM providers ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderConfig, 0, 8)
	for rows.Next() {
		var p ProviderConfig
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &created); err != nil {
			return nil, err
		}
		if p.CreatedAt, err = parseStoredTime(created); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProviderAccountConfigs returns provider accounts sorted by id.
func (s *Store) ListProviderAccountConfigs() ([]ProviderAccountConfig, error) {
	rows, err := s.db.Query(`
SELECT id, provider_id, created_at FROM provider_accounts ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderAccountConfig, 0, 16)
	for rows.Next() {
		var a ProviderAccountConfig
		var created string
		if err := rows.Scan(&a.ID, &a.ProviderID, &created); err != nil {
			return nil, err
		}
		if a.CreatedAt, err = parseStoredTime(created); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// parseStoredTime accepts the RFC3339Nano / RFC3339 text the store writes.
func parseStoredTime(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		t, err = time.Parse(time.RFC3339, v)
	}
	return t, err
}
