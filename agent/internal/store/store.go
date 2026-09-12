package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	ListenPorts []int               `json:"listen_ports,omitempty"`
	Protect     *ProtectProfile     `json:"protect,omitempty"`
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
			return State{Servers: []Server{}, Balances: map[string]string{}}, nil
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
	return st, nil
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
		if existing.Backend == backend && existing.Name == name {
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

// Save persists the full state document.
func (s *Store) Save(st State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.Servers == nil {
		st.Servers = []Server{}
	}
	return s.saveUnlocked(st)
}

func (s *Store) saveUnlocked(st State) error {
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
