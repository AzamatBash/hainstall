package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RemnaNodeCatalog is a Remnawave node synced into hapanel for analytics.
type RemnaNodeCatalog struct {
	PanelID            string `json:"panel_id"`
	PanelName          string `json:"panel_name,omitempty"`
	RemnaUUID          string `json:"remna_uuid"`
	Name               string `json:"name"`
	Address            string `json:"address"`
	ConfigProfileUUID  string `json:"config_profile_uuid"`
	InboundUUIDsJSON   string `json:"-"`
	InboundTags        string `json:"inbound_tags"`
	ProtocolDerived    string `json:"protocol_derived"`
	ProtocolOverride   string `json:"protocol_override"`
	Protocol           string `json:"protocol"` // effective: override || derived
	RoleRNFront        bool   `json:"role_rn_front"`
	RoleRNBack         bool   `json:"role_rn_back"`
	RoleHPFront        bool   `json:"role_hp_front"`
	RoleHPBack         bool   `json:"role_hp_back"`
	RoleCDNBack        bool   `json:"role_cdn_back"`
	EnabledInAnalytics bool   `json:"enabled_in_analytics"`
	Notes              string `json:"notes"`
	UsersOnline        int    `json:"users_online"`
	NodeOK             bool   `json:"node_ok"`
	LastSeenAt         int64  `json:"last_seen_at"`
	UpdatedAt          int64  `json:"updated_at"`
}

// RemnaNodeSyncInput is poller-written fields (does not overwrite roles/override).
type RemnaNodeSyncInput struct {
	PanelID           string
	RemnaUUID         string
	Name              string
	Address           string
	ConfigProfileUUID string
	InboundUUIDs      []string
	InboundTags       []string
	ProtocolDerived   string
	UsersOnline       int
	NodeOK            bool
	At                time.Time
}

// RemnaNodeOnlineSample is per-node usersOnline history.
type RemnaNodeOnlineSample struct {
	PanelID   string `json:"panel_id"`
	RemnaUUID string `json:"remna_uuid"`
	TS        int64  `json:"t"`
	Online    int    `json:"online"`
	NodeOK    bool   `json:"node_ok"`
}

const remnaNodeOnlineRetention = 14 * 24 * time.Hour

// UpsertRemnaNodeCatalogSync updates Remna-sourced fields; preserves roles/override/enabled/notes.
func (s *Store) UpsertRemnaNodeCatalogSync(in RemnaNodeSyncInput) error {
	if in.PanelID == "" || in.RemnaUUID == "" {
		return nil
	}
	uuidsJSON, _ := json.Marshal(in.InboundUUIDs)
	tags := strings.Join(in.InboundTags, ", ")
	proto := strings.TrimSpace(in.ProtocolDerived)
	if proto == "" {
		proto = "unknown"
	}
	online := in.UsersOnline
	if online < 0 {
		online = 0
	}
	ts := in.At.UTC().UnixMilli()
	nodeOK := 0
	if in.NodeOK {
		nodeOK = 1
	}
	_, err := s.db.Exec(`
INSERT INTO remna_node_catalog (
  panel_id, remna_uuid, name, address, config_profile_uuid,
  inbound_uuids_json, inbound_tags, protocol_derived,
  users_online, node_ok, last_seen_at, updated_at,
  protocol_override, role_rn_front, role_rn_back, role_hp_back,
  enabled_in_analytics, notes
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 0, 0, 0, 1, '')
ON CONFLICT(panel_id, remna_uuid) DO UPDATE SET
  name = excluded.name,
  address = excluded.address,
  config_profile_uuid = excluded.config_profile_uuid,
  inbound_uuids_json = excluded.inbound_uuids_json,
  inbound_tags = excluded.inbound_tags,
  protocol_derived = excluded.protocol_derived,
  users_online = excluded.users_online,
  node_ok = excluded.node_ok,
  last_seen_at = excluded.last_seen_at,
  updated_at = excluded.updated_at
`, in.PanelID, in.RemnaUUID, in.Name, in.Address, in.ConfigProfileUUID,
		string(uuidsJSON), tags, proto, online, nodeOK, ts, ts)
	return err
}

// AppendRemnaNodeOnlineSample stores one per-node sample and prunes old rows for that node.
func (s *Store) AppendRemnaNodeOnlineSample(panelID, remnaUUID string, at time.Time, online int, nodeOK bool) error {
	if panelID == "" || remnaUUID == "" {
		return nil
	}
	if online < 0 {
		online = 0
	}
	ts := at.UTC().UnixMilli()
	cutoff := at.UTC().Add(-remnaNodeOnlineRetention).UnixMilli()
	ok := 0
	if nodeOK {
		ok = 1
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`
INSERT OR REPLACE INTO remna_node_online_samples (panel_id, remna_uuid, ts, online, node_ok)
VALUES (?, ?, ?, ?, ?)`, panelID, remnaUUID, ts, online, ok); err != nil {
		return err
	}
	if _, err := tx.Exec(`
DELETE FROM remna_node_online_samples
WHERE panel_id = ? AND remna_uuid = ? AND ts < ?`, panelID, remnaUUID, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// RemnaNodePatch is a partial update for analytics attributes.
type RemnaNodePatch struct {
	ProtocolOverride   *string
	RoleRNFront        *bool
	RoleRNBack         *bool
	RoleHPFront        *bool
	RoleHPBack         *bool
	RoleCDNBack        *bool
	EnabledInAnalytics *bool
	Notes              *string
}

// PatchRemnaNodeCatalog updates manual analytics fields.
func (s *Store) PatchRemnaNodeCatalog(panelID, remnaUUID string, p RemnaNodePatch) (*RemnaNodeCatalog, error) {
	cur, err := s.GetRemnaNodeCatalog(panelID, remnaUUID)
	if err != nil || cur == nil {
		return cur, err
	}
	if p.ProtocolOverride != nil {
		cur.ProtocolOverride = strings.TrimSpace(*p.ProtocolOverride)
	}
	if p.RoleRNFront != nil {
		cur.RoleRNFront = *p.RoleRNFront
	}
	if p.RoleRNBack != nil {
		cur.RoleRNBack = *p.RoleRNBack
	}
	if p.RoleHPFront != nil {
		cur.RoleHPFront = *p.RoleHPFront
	}
	if p.RoleHPBack != nil {
		cur.RoleHPBack = *p.RoleHPBack
	}
	if p.RoleCDNBack != nil {
		cur.RoleCDNBack = *p.RoleCDNBack
	}
	if p.EnabledInAnalytics != nil {
		cur.EnabledInAnalytics = *p.EnabledInAnalytics
	}
	if p.Notes != nil {
		cur.Notes = *p.Notes
	}
	now := time.Now().UTC().UnixMilli()
	_, err = s.db.Exec(`
UPDATE remna_node_catalog SET
  protocol_override = ?,
  role_rn_front = ?, role_rn_back = ?, role_hp_front = ?, role_hp_back = ?, role_cdn_back = ?,
  enabled_in_analytics = ?, notes = ?, updated_at = ?
WHERE panel_id = ? AND remna_uuid = ?`,
		cur.ProtocolOverride,
		boolInt(cur.RoleRNFront), boolInt(cur.RoleRNBack),
		boolInt(cur.RoleHPFront), boolInt(cur.RoleHPBack), boolInt(cur.RoleCDNBack),
		boolInt(cur.EnabledInAnalytics), cur.Notes, now,
		panelID, remnaUUID,
	)
	if err != nil {
		return nil, err
	}
	return s.GetRemnaNodeCatalog(panelID, remnaUUID)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func scanRemnaNodeCatalog(scanner interface {
	Scan(dest ...any) error
}) (*RemnaNodeCatalog, error) {
	var n RemnaNodeCatalog
	var roleF, roleB, roleHPF, roleHPB, roleCDN, enabled, nodeOK int
	err := scanner.Scan(
		&n.PanelID, &n.PanelName, &n.RemnaUUID, &n.Name, &n.Address,
		&n.ConfigProfileUUID, &n.InboundUUIDsJSON, &n.InboundTags,
		&n.ProtocolDerived, &n.ProtocolOverride,
		&roleF, &roleB, &roleHPF, &roleHPB, &roleCDN, &enabled, &n.Notes,
		&n.UsersOnline, &nodeOK, &n.LastSeenAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	n.RoleRNFront = roleF != 0
	n.RoleRNBack = roleB != 0
	n.RoleHPFront = roleHPF != 0
	n.RoleHPBack = roleHPB != 0
	n.RoleCDNBack = roleCDN != 0
	n.EnabledInAnalytics = enabled != 0
	n.NodeOK = nodeOK != 0
	n.Protocol = effectiveProtocol(n.ProtocolOverride, n.ProtocolDerived)
	return &n, nil
}

func effectiveProtocol(override, derived string) string {
	o := strings.TrimSpace(override)
	if o != "" {
		return o
	}
	d := strings.TrimSpace(derived)
	if d == "" {
		return "unknown"
	}
	return d
}

const remnaNodeSelect = `
SELECT c.panel_id, COALESCE(p.name, ''), c.remna_uuid, c.name, c.address,
  c.config_profile_uuid, c.inbound_uuids_json, c.inbound_tags,
  c.protocol_derived, c.protocol_override,
  c.role_rn_front, c.role_rn_back, c.role_hp_front, c.role_hp_back, c.role_cdn_back,
  c.enabled_in_analytics, c.notes,
  c.users_online, c.node_ok, c.last_seen_at, c.updated_at
FROM remna_node_catalog c
LEFT JOIN remna_panels p ON p.id = c.panel_id
`

// ListRemnaNodeCatalog returns all catalog nodes across panels.
func (s *Store) ListRemnaNodeCatalog() ([]RemnaNodeCatalog, error) {
	rows, err := s.db.Query(remnaNodeSelect + ` ORDER BY p.name ASC, c.name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RemnaNodeCatalog, 0, 64)
	for rows.Next() {
		n, err := scanRemnaNodeCatalog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// GetRemnaNodeCatalog returns one catalog row.
func (s *Store) GetRemnaNodeCatalog(panelID, remnaUUID string) (*RemnaNodeCatalog, error) {
	row := s.db.QueryRow(remnaNodeSelect+` WHERE c.panel_id = ? AND c.remna_uuid = ?`, panelID, remnaUUID)
	n, err := scanRemnaNodeCatalog(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return n, nil
}

// AnalyticsBucket is a time series point for a group key.
type AnalyticsBucket struct {
	TS    int64  `json:"t"`
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Online int   `json:"online"`
}

// AnalyticsNodeRank is a top-node row for the week view.
type AnalyticsNodeRank struct {
	PanelID   string `json:"panel_id"`
	PanelName string `json:"panel_name"`
	RemnaUUID string `json:"remna_uuid"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Online    int    `json:"online"`
}

// WeekAnalytics is consolidated analytics across all Remna panels.
type WeekAnalytics struct {
	RangeHours     int                 `json:"range_hours"`
	BucketMs       int64               `json:"bucket_ms"`
	BySegment      []AnalyticsBucket   `json:"by_segment"`
	ByProtocol     []AnalyticsBucket   `json:"by_protocol"`
	ByRole         []AnalyticsBucket   `json:"by_role"`
	TopNodes       []AnalyticsNodeRank `json:"top_nodes"`
	Top3SharePct   float64             `json:"top3_share_pct"`
	TotalOnlineNow int                 `json:"total_online_now"`
}

// analyticsSegments returns QoS buckets for a node (may be multiple).
func analyticsSegments(n RemnaNodeCatalog) []string {
	proto := strings.ToLower(strings.TrimSpace(n.Protocol))
	isVless := proto == "vless_reality" || proto == "vless"
	isHy2 := proto == "hysteria2" || proto == "hysteria" || proto == "hy2"
	out := make([]string, 0, 3)
	if isVless && n.RoleHPFront {
		out = append(out, "vless_reality_hp_front")
	} else if isVless {
		out = append(out, "vless_reality")
	}
	if isHy2 {
		out = append(out, "hysteria2")
	}
	if n.RoleCDNBack {
		out = append(out, "cdn")
	}
	return out
}

func segmentLabel(key string) string {
	switch key {
	case "vless_reality":
		return "VLESS Reality"
	case "hysteria2":
		return "Hysteria"
	case "vless_reality_hp_front":
		return "VLESS Reality + HP front"
	case "cdn":
		return "CDN"
	default:
		return key
	}
}

// BuildWeekAnalytics aggregates per-node samples with catalog attributes.
func (s *Store) BuildWeekAnalytics(since time.Time) (*WeekAnalytics, error) {
	nodes, err := s.ListRemnaNodeCatalog()
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]RemnaNodeCatalog, len(nodes))
	totalNow := 0
	for _, n := range nodes {
		if !n.EnabledInAnalytics {
			continue
		}
		key := n.PanelID + "\x00" + n.RemnaUUID
		enabled[key] = n
		totalNow += n.UsersOnline
	}

	cutoff := since.UTC().UnixMilli()
	rows, err := s.db.Query(`
SELECT panel_id, remna_uuid, ts, online FROM remna_node_online_samples
WHERE ts >= ?
ORDER BY ts ASC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Hourly buckets for week view: last sample per node per hour, then sum by group.
	const bucketMs = int64(60 * 60 * 1000)
	type nodeBucket struct {
		bucket int64
		node   string
	}
	lastInBucket := map[nodeBucket]struct {
		online int
		ts     int64
		node   RemnaNodeCatalog
	}{}
	latestOnline := map[string]int{}
	latestTS := map[string]int64{}

	for rows.Next() {
		var panelID, remnaUUID string
		var ts int64
		var online int
		if err := rows.Scan(&panelID, &remnaUUID, &ts, &online); err != nil {
			return nil, err
		}
		nk := panelID + "\x00" + remnaUUID
		n, ok := enabled[nk]
		if !ok {
			continue
		}
		b := (ts / bucketMs) * bucketMs
		key := nodeBucket{b, nk}
		prev, exists := lastInBucket[key]
		if !exists || ts >= prev.ts {
			lastInBucket[key] = struct {
				online int
				ts     int64
				node   RemnaNodeCatalog
			}{online: online, ts: ts, node: n}
		}
		if ts >= latestTS[nk] {
			latestTS[nk] = ts
			latestOnline[nk] = online
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type accKey struct {
		bucket int64
		group  string
	}
	protoAcc := map[accKey]int{}
	segmentAcc := map[accKey]int{}
	roleAcc := map[accKey]int{}
	for nb, v := range lastInBucket {
		n := v.node
		proto := n.Protocol
		if proto == "" {
			proto = "unknown"
		}
		b := nb.bucket
		protoAcc[accKey{b, proto}] += v.online
		for _, seg := range analyticsSegments(n) {
			segmentAcc[accKey{b, seg}] += v.online
		}
		if n.RoleRNFront {
			roleAcc[accKey{b, "rn_front"}] += v.online
		}
		if n.RoleRNBack {
			roleAcc[accKey{b, "rn_back"}] += v.online
		}
		if n.RoleHPFront {
			roleAcc[accKey{b, "hp_front"}] += v.online
		}
		if n.RoleHPBack {
			roleAcc[accKey{b, "hp_back"}] += v.online
		}
		if n.RoleCDNBack {
			roleAcc[accKey{b, "cdn_back"}] += v.online
		}
	}

	byProto := make([]AnalyticsBucket, 0, len(protoAcc))
	for k, v := range protoAcc {
		byProto = append(byProto, AnalyticsBucket{TS: k.bucket, Key: k.group, Online: v})
	}
	bySegment := make([]AnalyticsBucket, 0, len(segmentAcc))
	for k, v := range segmentAcc {
		bySegment = append(bySegment, AnalyticsBucket{
			TS: k.bucket, Key: k.group, Label: segmentLabel(k.group), Online: v,
		})
	}
	byRole := make([]AnalyticsBucket, 0, len(roleAcc))
	for k, v := range roleAcc {
		label := k.group
		switch k.group {
		case "rn_front":
			label = "RN front"
		case "rn_back":
			label = "RN back"
		case "hp_front":
			label = "HP front"
		case "hp_back":
			label = "HP back"
		case "cdn_back":
			label = "CDN back"
		}
		byRole = append(byRole, AnalyticsBucket{TS: k.bucket, Key: k.group, Label: label, Online: v})
	}

	ranks := make([]AnalyticsNodeRank, 0, len(latestOnline))
	sumAll := 0
	for nk, online := range latestOnline {
		n := enabled[nk]
		sumAll += online
		ranks = append(ranks, AnalyticsNodeRank{
			PanelID:   n.PanelID,
			PanelName: n.PanelName,
			RemnaUUID: n.RemnaUUID,
			Name:      n.Name,
			Protocol:  n.Protocol,
			Online:    online,
		})
	}
	// sort desc by online (simple insertion for small N)
	for i := 0; i < len(ranks); i++ {
		for j := i + 1; j < len(ranks); j++ {
			if ranks[j].Online > ranks[i].Online {
				ranks[i], ranks[j] = ranks[j], ranks[i]
			}
		}
	}
	top := ranks
	if len(top) > 10 {
		top = top[:10]
	}
	top3 := 0
	for i := 0; i < len(ranks) && i < 3; i++ {
		top3 += ranks[i].Online
	}
	share := 0.0
	if sumAll > 0 {
		share = float64(top3) * 100 / float64(sumAll)
	}

	hours := int(time.Since(since).Hours() + 0.5)
	if hours < 1 {
		hours = 168
	}
	return &WeekAnalytics{
		RangeHours:     hours,
		BucketMs:       bucketMs,
		BySegment:      bySegment,
		ByProtocol:     byProto,
		ByRole:         byRole,
		TopNodes:       top,
		Top3SharePct:   share,
		TotalOnlineNow: totalNow,
	}, nil
}
