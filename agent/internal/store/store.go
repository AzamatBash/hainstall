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

// State is the on-disk agent state restored across reloads.
type State struct {
	Servers []Server `json:"servers"`
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
			return State{Servers: []Server{}}, nil
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

// Upsert adds or updates a server and persists.
func (s *Store) Upsert(srv Server) error {
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
	if err := s.saveUnlocked(st); err != nil {
		return false, err
	}
	return true, nil
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
