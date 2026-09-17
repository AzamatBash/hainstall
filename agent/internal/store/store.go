package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Server is a persisted backend server entry.
type Server struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Weight  int    `json:"weight"`
}

// Entrance is one client listen port routed to a HAProxy backend.
type Entrance struct {
	Port    int    `json:"port"`
	Backend string `json:"backend"`
}

// ProtectProfile is HAProxy (and later host) protection settings.
type ProtectProfile struct {
	ClientHello         bool `json:"client_hello"`
	ClientHelloDelaySec int  `json:"client_hello_delay_sec"`
	RateLimit           bool `json:"rate_limit"`
	MaxConnPerIP        int  `json:"max_conn_per_ip"`
	MaxRatePerIP        int  `json:"max_rate_per_ip"`
	RateWindowSec       int  `json:"rate_window_sec"`
	RUOnly              bool `json:"ru_only"`
	SynProxy            bool `json:"synproxy"`
}

// State is the on-disk agent state restored across reloads.
type State struct {
	Servers     []Server            `json:"servers"`
	Balances    map[string]string   `json:"balances,omitempty"` // backend name -> HAProxy balance algorithm
	Entrances   []Entrance          `json:"entrances,omitempty"`
	ListenPorts []int               `json:"listen_ports,omitempty"` // derived from Entrances; kept for older agents/tools
	Protect     *ProtectProfile     `json:"protect,omitempty"`
}

// DefaultEntrance is used when nothing is configured.
var DefaultEntrance = Entrance{Port: 8443, Backend: "app"}

// NormalizeEntrances validates, sanitizes backends, dedupes ports (first wins), sorts by port.
func NormalizeEntrances(in []Entrance) ([]Entrance, error) {
	if len(in) == 0 {
		return []Entrance{DefaultEntrance}, nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]Entrance, 0, len(in))
	for _, e := range in {
		if e.Port < 1 || e.Port > 65535 {
			return nil, fmt.Errorf("invalid entrance port %d (want 1–65535)", e.Port)
		}
		if _, ok := seen[e.Port]; ok {
			continue
		}
		seen[e.Port] = struct{}{}
		backend := sanitizeBackendName(e.Backend)
		out = append(out, Entrance{Port: e.Port, Backend: backend})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// PortsFromEntrances returns sorted unique listen ports.
func PortsFromEntrances(ents []Entrance) []int {
	out := make([]int, len(ents))
	for i, e := range ents {
		out[i] = e.Port
	}
	return out
}

// EntrancesFromPorts builds one entrance per port, preserving backends from prev when possible.
func EntrancesFromPorts(ports []int, prev []Entrance, defaultBackend string) []Entrance {
	if defaultBackend == "" {
		defaultBackend = "app"
	}
	byPort := make(map[int]string, len(prev))
	for _, e := range prev {
		byPort[e.Port] = e.Backend
	}
	out := make([]Entrance, 0, len(ports))
	for _, p := range ports {
		backend := defaultBackend
		if b, ok := byPort[p]; ok && b != "" {
			backend = b
		}
		out = append(out, Entrance{Port: p, Backend: backend})
	}
	return out
}

// sanitizeBackendName mirrors HAProxy-safe ids (same rules as server names).
func sanitizeBackendName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "app"
	}
	return sanitizeServerName(name)
}

// syncDerivedPorts sets ListenPorts from Entrances.
func syncDerivedPorts(st *State) {
	st.ListenPorts = PortsFromEntrances(st.Entrances)
}

// Store persists backend server inventory to a JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a store backed by path. Parent directories are created on first write.
func New(path string) *Store {
	return &Store{path: path}
}

// Load reads state from disk. Missing file yields empty state.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUnlocked()
}

func (s *Store) loadUnlocked() (State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return State{
				Servers:   []Server{},
				Balances:  map[string]string{},
				Entrances: []Entrance{DefaultEntrance},
				ListenPorts: []int{DefaultEntrance.Port},
			}, nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	if st.Servers == nil {
		st.Servers = []Server{}
	}
	if st.Balances == nil {
		st.Balances = map[string]string{}
	}
	st = hydrateEntrances(st)
	return st, nil
}

// hydrateEntrances fills Entrances from ListenPorts (legacy) or defaults.
func hydrateEntrances(st State) State {
	if len(st.Entrances) > 0 {
		if ents, err := NormalizeEntrances(st.Entrances); err == nil {
			st.Entrances = ents
			syncDerivedPorts(&st)
			return st
		}
	}
	if len(st.ListenPorts) > 0 {
		st.Entrances = EntrancesFromPorts(st.ListenPorts, nil, "app")
		if ents, err := NormalizeEntrances(st.Entrances); err == nil {
			st.Entrances = ents
		}
		syncDerivedPorts(&st)
		return st
	}
	st.Entrances = []Entrance{DefaultEntrance}
	syncDerivedPorts(&st)
	return st
}

// List returns a copy of all servers.
func (s *Store) List() ([]Server, error) {
	st, err := s.Load()
	if err != nil {
		return nil, err
	}
	out := make([]Server, len(st.Servers))
	copy(out, st.Servers)
	return out, nil
}

// Balances returns a copy of per-backend balance algorithms.
func (s *Store) Balances() (map[string]string, error) {
	st, err := s.Load()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(st.Balances))
	for k, v := range st.Balances {
		out[k] = v
	}
	return out, nil
}

// Upsert adds or updates a server and persists.
func (s *Store) Upsert(srv Server) error {
	return s.UpsertWithBalance(srv, "")
}

// UpsertWithBalance upserts a server and, when balance is non-empty, sets the
// HAProxy balance algorithm for that backend name.
func (s *Store) UpsertWithBalance(srv Server, balance string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	found := false
	for i, existing := range st.Servers {
		if existing.Backend == srv.Backend && existing.Name == srv.Name {
			st.Servers[i] = srv
			found = true
			break
		}
	}
	if !found {
		st.Servers = append(st.Servers, srv)
	}
	if balance != "" {
		if st.Balances == nil {
			st.Balances = map[string]string{}
		}
		st.Balances[srv.Backend] = balance
	}
	return s.saveUnlocked(st)
}

// Delete removes a server and persists. Returns true if something was removed.
// name may be the stored name or its HAProxy-sanitized form.
func (s *Store) Delete(backend, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return false, err
	}
	next := make([]Server, 0, len(st.Servers))
	removed := false
	for _, existing := range st.Servers {
		if existing.Backend == backend && serverNameMatch(existing.Name, name) {
			removed = true
			continue
		}
		next = append(next, existing)
	}
	if !removed {
		return false, nil
	}
	st.Servers = next
	still := false
	for _, existing := range st.Servers {
		if existing.Backend == backend {
			still = true
			break
		}
	}
	if !still && st.Balances != nil {
		delete(st.Balances, backend)
	}
	if err := s.saveUnlocked(st); err != nil {
		return false, err
	}
	return true, nil
}

// serverNameMatch treats raw and HAProxy-safe forms as the same identity.
func serverNameMatch(stored, want string) bool {
	if stored == want {
		return true
	}
	// Lazy import avoided: compare common sanitized forms used by haproxy.SanitizeName.
	return sanitizeServerName(stored) == sanitizeServerName(want)
}

// sanitizeServerName mirrors haproxy.SanitizeName for store matching/migration.
func sanitizeServerName(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	b.Grow(len(name))
	prevUnderscore := false
	for _, r := range name {
		ok := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if ok {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "._-")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	if out == "" {
		return "srv"
	}
	return out
}

// MigrateSanitizeNames rewrites server names to HAProxy-safe form. Returns how many changed.
func (s *Store) MigrateSanitizeNames() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadUnlocked()
	if err != nil {
		return 0, err
	}
	changed := 0
	for i, srv := range st.Servers {
		clean := sanitizeServerName(srv.Name)
		if clean != srv.Name {
			st.Servers[i].Name = clean
			changed++
		}
	}
	if changed == 0 {
		return 0, nil
	}
	if err := s.saveUnlocked(st); err != nil {
		return 0, err
	}
	return changed, nil
}

// Entrances returns a copy of configured entrances (always non-empty after hydrate).
func (s *Store) Entrances() ([]Entrance, error) {
	st, err := s.Load()
	if err != nil {
		return nil, err
	}
	out := make([]Entrance, len(st.Entrances))
	copy(out, st.Entrances)
	return out, nil
}

// SetEntrances persists normalized entrances and syncs ListenPorts.
func (s *Store) SetEntrances(ents []Entrance) ([]Entrance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	norm, err := NormalizeEntrances(ents)
	if err != nil {
		return nil, err
	}
	st.Entrances = norm
	syncDerivedPorts(&st)
	if err := s.saveUnlocked(st); err != nil {
		return nil, err
	}
	return norm, nil
}

// MigrateEntrances persists hydrated entrances when the on-disk file lacked them.
// Returns true if state was rewritten.
func (s *Store) MigrateEntrances() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	if _, ok := raw["entrances"]; ok {
		return false, nil
	}
	st, err := s.loadUnlocked()
	if err != nil {
		return false, err
	}
	if err := s.saveUnlocked(st); err != nil {
		return false, err
	}
	return true, nil
}

// Save persists the full state document.
func (s *Store) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.Servers == nil {
		st.Servers = []Server{}
	}
	st = hydrateEntrances(st)
	return s.saveUnlocked(st)
}

func (s *Store) saveUnlocked(st State) error {
	if len(st.Entrances) == 0 {
		st = hydrateEntrances(st)
	} else {
		syncDerivedPorts(&st)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}
