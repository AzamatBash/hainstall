package api

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/buildinfo"
	"github.com/azabash/hapanel/panel/internal/store"
)

const configSchemaV1 = "hapanel.config.v1"

// requireAPIKeyScope allows a panel key that has scope, or admin JWT (cookie or
// Bearer) like requireAPIKeyOrAuth. A valid key without the scope (including
// PANEL_API_KEY, which never has scopes) gets 403; anything else gets 401.
func (s *Server) requireAPIKeyScope(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		keyValid, hasScope := s.apiKeyHasScope(r, scope)
		if hasScope {
			next(w, r)
			return
		}
		tok := s.auth.TokenFromRequest(r)
		if tok != "" && s.auth.ParseToken(tok) == nil {
			next(w, r)
			return
		}
		if keyValid {
			writeErr(w, http.StatusForbidden, "у ключа нет права "+scope)
			return
		}
		writeErr(w, http.StatusUnauthorized, "нет доступа")
	}
}

// apiKeyHasScope reports whether the request carries a valid API key and
// whether that key has scope. One key lookup (and last_used_at update) per call.
func (s *Server) apiKeyHasScope(r *http.Request, scope string) (valid, has bool) {
	got := s.apiKeyFromRequest(r)
	if got == "" {
		return false, false
	}
	if s.apiKey != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.apiKey)) == 1 {
		return true, false
	}
	meta, ok := s.store.LookupAPIKey(got)
	if !ok {
		return false, false
	}
	return true, meta.HasScope(scope)
}

// configSnapshot is the GET /api/v1/config response (schema hapanel.config.v1).
type configSnapshot struct {
	Schema       string         `json:"schema"`
	Version      string         `json:"version"`
	GeneratedAt  string         `json:"generated_at"`
	SnapshotHash string         `json:"snapshot_hash"`
	Sections     configSections `json:"sections"`
}

type configSections struct {
	Hanodes          configSection `json:"hanodes"`
	Links            configSection `json:"links"`
	RemnaPanels      configSection `json:"remna_panels"`
	RemnaNodes       configSection `json:"remna_nodes"`
	Providers        configSection `json:"providers"`
	ProviderAccounts configSection `json:"provider_accounts"`
}

type configSection struct {
	Status      string `json:"status"`
	ContentHash string `json:"content_hash,omitempty"`
	Items       any    `json:"items"`
}

type configHanode struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	Country            string           `json:"country"`
	Host               string           `json:"host"`
	SortOrder          int              `json:"sort_order"`
	CreatedAt          string           `json:"created_at"`
	ListenPorts        []int            `json:"listen_ports"`
	Entrances          []configEntrance `json:"entrances"`
	Protect            configProtect    `json:"protect"`
	ProtectDefault     bool             `json:"protect_default"`
	RemnaPanelID       *string          `json:"remna_panel_id"`
	ProviderID         *string          `json:"provider_id"`
	ProviderAccountID  *string          `json:"provider_account_id"`
	TrafficLog         bool             `json:"traffic_log"`
	HasToken           bool             `json:"has_token"`
	Backends           []configBackend  `json:"backends"`
	BackendsObservedAt *string          `json:"backends_observed_at"`
}

type configEntrance struct {
	Port    int    `json:"port"`
	Backend string `json:"backend"`
}

type configProtect struct {
	ClientHello         bool `json:"client_hello"`
	ClientHelloDelaySec int  `json:"client_hello_delay_sec"`
	RateLimit           bool `json:"rate_limit"`
	MaxConnPerIP        int  `json:"max_conn_per_ip"`
	MaxRatePerIP        int  `json:"max_rate_per_ip"`
	RateWindowSec       int  `json:"rate_window_sec"`
	RUOnly              bool `json:"ru_only"`
	SynProxy            bool `json:"synproxy"`
}

type configBackend struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

type configLink struct {
	NodeID       string  `json:"node_id"`
	Backend      string  `json:"backend"`
	ServerName   string  `json:"server_name"`
	RemnaPanelID string  `json:"remna_panel_id"`
	RemnaAddress string  `json:"remna_address"`
	RemnaUUID    *string `json:"remna_uuid"`
	Match        string  `json:"match"`
	Active       bool    `json:"active"`
}

type configRemnaPanel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	HasAPIKey bool   `json:"has_api_key"`
	CreatedAt string `json:"created_at"`
}

type configRemnaNode struct {
	PanelID            string          `json:"panel_id"`
	UUID               string          `json:"uuid"`
	Name               string          `json:"name"`
	Address            string          `json:"address"`
	ConfigProfileUUID  string          `json:"config_profile_uuid"`
	InboundUUIDs       []string        `json:"inbound_uuids"`
	InboundTags        []string        `json:"inbound_tags"`
	Protocol           string          `json:"protocol"`
	ProtocolDerived    string          `json:"protocol_derived"`
	ProtocolOverride   string          `json:"protocol_override"`
	Roles              configRemnaRole `json:"roles"`
	EnabledInAnalytics bool            `json:"enabled_in_analytics"`
	Notes              string          `json:"notes"`
	AlertNote          string          `json:"alert_note"`
}

type configRemnaRole struct {
	RNFront bool `json:"rn_front"`
	RNBack  bool `json:"rn_back"`
	HPFront bool `json:"hp_front"`
	HPBack  bool `json:"hp_back"`
	CDNBack bool `json:"cdn_back"`
}

type configProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type configProviderAccount struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`
	CreatedAt  string `json:"created_at"`
}

// handleExportConfig returns a read-only configuration snapshot for external
// tools (VPN expert). It reads SQLite only: no agent or Remnawave calls.
//
// GET /api/v1/config (API key with scope "config" or admin JWT)
func (s *Server) handleExportConfig(w http.ResponseWriter, r *http.Request) {
	snap, err := s.buildConfigSnapshot(time.Now().UTC())
	if err != nil {
		s.logger.Error("export config hash", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка сериализации")
		return
	}
	w.Header().Set("ETag", `"`+snap.SnapshotHash+`"`)
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) buildConfigSnapshot(now time.Time) (configSnapshot, error) {
	nodes, nodesErr := s.store.ListNodeConfigs()
	catalog, catalogErr := s.store.ListRemnaNodeConfigs()
	links, linksErr := s.store.ListAllBackendRemnaLinks()
	panels, panelsErr := s.store.ListRemnaPanelConfigs()
	providers, providersErr := s.store.ListProviderConfigs()
	accounts, accountsErr := s.store.ListProviderAccountConfigs()

	// Links need hanode snapshots (active) and the catalog (remna_uuid/match);
	// without them the section would be wrong, so it fails with them.
	if linksErr == nil && nodesErr != nil {
		linksErr = nodesErr
	}
	if linksErr == nil && catalogErr != nil {
		linksErr = catalogErr
	}

	var sec configSections
	sec.Hanodes = s.configSection("hanodes", nodesErr, func() any { return configHanodeItems(nodes) })
	sec.Links = s.configSection("links", linksErr, func() any { return configLinkItems(links, nodes, catalog) })
	sec.RemnaPanels = s.configSection("remna_panels", panelsErr, func() any { return configRemnaPanelItems(panels) })
	sec.RemnaNodes = s.configSection("remna_nodes", catalogErr, func() any { return configRemnaNodeItems(catalog) })
	sec.Providers = s.configSection("providers", providersErr, func() any { return configProviderItems(providers) })
	sec.ProviderAccounts = s.configSection("provider_accounts", accountsErr, func() any { return configProviderAccountItems(accounts) })

	raw, err := canonicalJSON(sec)
	if err != nil {
		return configSnapshot{}, err
	}
	return configSnapshot{
		Schema:       configSchemaV1,
		Version:      buildinfo.Version,
		GeneratedAt:  now.UTC().Format(time.RFC3339),
		SnapshotHash: sha256Tag(raw),
		Sections:     sec,
	}, nil
}

// configSection wraps items with status and content hash; a failed query
// (or hash) gives status "error", empty items and no hash.
func (s *Server) configSection(name string, err error, items func() any) configSection {
	if err != nil {
		s.logger.Error("export config section", "section", name, "err", err)
		return configSection{Status: "error", Items: []any{}}
	}
	v := items()
	raw, err := canonicalJSON(v)
	if err != nil {
		s.logger.Error("export config section hash", "section", name, "err", err)
		return configSection{Status: "error", Items: []any{}}
	}
	return configSection{Status: "ok", ContentHash: sha256Tag(raw), Items: v}
}

// canonicalJSON encodes v with object keys sorted at every level, no
// insignificant whitespace and no HTML escaping (UTF-8 as is). Hashes in the
// config export are SHA-256 of this form.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(generic); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func sha256Tag(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func configTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func configOptionalID(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}

func configHanodeItems(nodes []store.NodeConfig) []configHanode {
	out := make([]configHanode, 0, len(nodes))
	for _, n := range nodes {
		ports := make([]int, 0, len(n.ListenPorts))
		ports = append(ports, n.ListenPorts...)
		ents := make([]configEntrance, 0, len(n.Entrances))
		for _, e := range n.Entrances {
			ents = append(ents, configEntrance{Port: e.Port, Backend: e.Backend})
		}
		backends := make([]configBackend, 0, len(n.Backends))
		for _, b := range n.Backends {
			// Same rule as the panel UI/API: management backends are hidden.
			if isInternalBackend(b.Backend) {
				continue
			}
			backends = append(backends, configBackend{Backend: b.Backend, Name: b.Name, Address: b.Address, Port: b.Port})
		}
		sort.SliceStable(backends, func(i, j int) bool {
			a, b := backends[i], backends[j]
			if a.Backend != b.Backend {
				return a.Backend < b.Backend
			}
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			if a.Address != b.Address {
				return a.Address < b.Address
			}
			return a.Port < b.Port
		})
		item := configHanode{
			ID:          n.ID,
			Name:        n.Name,
			Country:     n.Country,
			Host:        n.Host,
			SortOrder:   n.SortOrder,
			CreatedAt:   configTime(n.CreatedAt),
			ListenPorts: ports,
			Entrances:   ents,
			Protect: configProtect{
				ClientHello:         n.Protect.ClientHello,
				ClientHelloDelaySec: n.Protect.ClientHelloDelaySec,
				RateLimit:           n.Protect.RateLimit,
				MaxConnPerIP:        n.Protect.MaxConnPerIP,
				MaxRatePerIP:        n.Protect.MaxRatePerIP,
				RateWindowSec:       n.Protect.RateWindowSec,
				RUOnly:              n.Protect.RUOnly,
				SynProxy:            n.Protect.SynProxy,
			},
			ProtectDefault:    n.ProtectDefault,
			RemnaPanelID:      configOptionalID(n.RemnaPanelID),
			ProviderID:        configOptionalID(n.ProviderID),
			ProviderAccountID: configOptionalID(n.ProviderAccountID),
			TrafficLog:        n.TrafficLog,
			HasToken:          n.HasToken,
			Backends:          backends,
		}
		if n.BackendsObservedAt != nil {
			at := configTime(*n.BackendsObservedAt)
			item.BackendsObservedAt = &at
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// remnaAddressKey is the address comparison of remna.FindNodeByAddress
// (trimmed, case-insensitive), applied to the stored catalog.
func remnaAddressKey(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

func configLinkItems(links []store.BackendRemnaLink, nodes []store.NodeConfig, catalog []store.RemnaNodeConfig) []configLink {
	type linkKey struct {
		node, backend, server string
	}
	snapshots := make(map[string]*store.NodeSnapshot, len(nodes))
	for _, n := range nodes {
		snapshots[n.ID] = &store.NodeSnapshot{Backends: n.Backends}
	}
	byNode := map[string][]store.BackendRemnaLink{}
	for _, l := range links {
		byNode[l.NodeID] = append(byNode[l.NodeID], l)
	}
	active := make(map[linkKey]bool, len(links))
	for nodeID, nodeLinks := range byNode {
		snap, ok := snapshots[nodeID]
		if !ok {
			continue // link of a node that no longer exists: never active
		}
		for _, l := range activeRemnaLinks(nodeLinks, snap) {
			active[linkKey{l.NodeID, l.Backend, l.ServerName}] = true
		}
	}
	byPanelAddr := map[string][]string{}
	for _, c := range catalog {
		key := remnaAddressKey(c.Address)
		if key == "" {
			continue
		}
		pk := c.PanelID + "\x00" + key
		byPanelAddr[pk] = append(byPanelAddr[pk], c.RemnaUUID)
	}

	out := make([]configLink, 0, len(links))
	for _, l := range links {
		item := configLink{
			NodeID:       l.NodeID,
			Backend:      l.Backend,
			ServerName:   l.ServerName,
			RemnaPanelID: l.RemnaPanelID,
			RemnaAddress: l.RemnaAddress,
			Match:        "none",
			Active:       active[linkKey{l.NodeID, l.Backend, l.ServerName}],
		}
		if key := remnaAddressKey(l.RemnaAddress); key != "" {
			switch uuids := byPanelAddr[l.RemnaPanelID+"\x00"+key]; len(uuids) {
			case 0:
			case 1:
				u := uuids[0]
				item.RemnaUUID = &u
				item.Match = "unique"
			default:
				item.Match = "ambiguous"
			}
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.Backend != b.Backend {
			return a.Backend < b.Backend
		}
		return a.ServerName < b.ServerName
	})
	return out
}

func configRemnaPanelItems(panels []store.RemnaPanelConfig) []configRemnaPanel {
	out := make([]configRemnaPanel, 0, len(panels))
	for _, p := range panels {
		out = append(out, configRemnaPanel{ID: p.ID, Name: p.Name, HasAPIKey: p.HasAPIKey, CreatedAt: configTime(p.CreatedAt)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func configRemnaNodeItems(catalog []store.RemnaNodeConfig) []configRemnaNode {
	out := make([]configRemnaNode, 0, len(catalog))
	for _, c := range catalog {
		uuids := make([]string, 0, len(c.InboundUUIDs))
		uuids = append(uuids, c.InboundUUIDs...)
		tags := make([]string, 0, len(c.InboundTags))
		tags = append(tags, c.InboundTags...)
		out = append(out, configRemnaNode{
			PanelID:           c.PanelID,
			UUID:              c.RemnaUUID,
			Name:              c.Name,
			Address:           c.Address,
			ConfigProfileUUID: c.ConfigProfileUUID,
			InboundUUIDs:      uuids,
			InboundTags:       tags,
			Protocol:          c.Protocol,
			ProtocolDerived:   c.ProtocolDerived,
			ProtocolOverride:  c.ProtocolOverride,
			Roles: configRemnaRole{
				RNFront: c.RoleRNFront,
				RNBack:  c.RoleRNBack,
				HPFront: c.RoleHPFront,
				HPBack:  c.RoleHPBack,
				CDNBack: c.RoleCDNBack,
			},
			EnabledInAnalytics: c.EnabledInAnalytics,
			Notes:              c.Notes,
			AlertNote:          c.AlertNote,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].PanelID != out[j].PanelID {
			return out[i].PanelID < out[j].PanelID
		}
		return out[i].UUID < out[j].UUID
	})
	return out
}

func configProviderItems(providers []store.ProviderConfig) []configProvider {
	out := make([]configProvider, 0, len(providers))
	for _, p := range providers {
		out = append(out, configProvider{ID: p.ID, Name: p.Name, CreatedAt: configTime(p.CreatedAt)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func configProviderAccountItems(accounts []store.ProviderAccountConfig) []configProviderAccount {
	out := make([]configProviderAccount, 0, len(accounts))
	for _, a := range accounts {
		out = append(out, configProviderAccount{ID: a.ID, ProviderID: a.ProviderID, CreatedAt: configTime(a.CreatedAt)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
