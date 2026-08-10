package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	StatusRunning = "running"
	StatusStopped = "stopped"
)

var (
	ErrNotFound = errors.New("not found")
	ErrInvalid  = errors.New("invalid")
)

// Instance is one tunnel/room endpoint owned by this agent node.
type Instance struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`  // jitsi|telemost|wbstream
	Transport string `json:"transport"` // datachannel|vp8channel|seichannel|videochannel
	RoomID    string `json:"room_id"`
	KeyHex    string `json:"key_hex"`
	Comment   string `json:"comment"`
	Enabled   bool   `json:"enabled"`
	Status    string `json:"status"` // running|stopped
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// State is the on-disk agent state.
type State struct {
	Deployed  bool       `json:"deployed"`
	Instances []Instance `json:"instances"`
}

// Store persists instances to a JSON file.
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
			return State{Instances: []Instance{}}, nil
		}
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parse state: %w", err)
	}
	if st.Instances == nil {
		st.Instances = []Instance{}
	}
	return st, nil
}

func (s *Store) saveUnlocked(st State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	if st.Instances == nil {
		st.Instances = []Instance{}
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

// List returns a copy of all instances.
func (s *Store) List() ([]Instance, error) {
	st, err := s.Load()
	if err != nil {
		return nil, err
	}
	out := make([]Instance, len(st.Instances))
	copy(out, st.Instances)
	return out, nil
}

// Get returns an instance by id.
func (s *Store) Get(id string) (*Instance, error) {
	st, err := s.Load()
	if err != nil {
		return nil, err
	}
	for i := range st.Instances {
		if st.Instances[i].ID == id {
			inst := st.Instances[i]
			return &inst, nil
		}
	}
	return nil, ErrNotFound
}

// CreateInput is the validated create payload.
type CreateInput struct {
	Name      string
	Provider  string
	Transport string
	RoomID    string
	KeyHex    string
	Comment   string
}

// Create validates, persists, and returns a new instance.
func (s *Store) Create(in CreateInput) (*Instance, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	room := strings.TrimSpace(in.RoomID)
	if room == "" {
		return nil, fmt.Errorf("%w: room_id is required", ErrInvalid)
	}
	key := strings.TrimSpace(in.KeyHex)
	if key == "" {
		k, err := randomKeyHex()
		if err != nil {
			return nil, err
		}
		key = k
	}
	if err := ValidateInstanceFields(in.Provider, in.Transport, key); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	inst := Instance{
		ID:        newID(),
		Name:      name,
		Provider:  strings.TrimSpace(in.Provider),
		Transport: strings.TrimSpace(in.Transport),
		RoomID:    room,
		KeyHex:    key,
		Comment:   strings.TrimSpace(in.Comment),
		Enabled:   true,
		Status:    StatusStopped,
		CreatedAt: now,
		UpdatedAt: now,
	}
	st.Instances = append(st.Instances, inst)
	if err := s.saveUnlocked(st); err != nil {
		return nil, err
	}
	return &inst, nil
}

// UpdateInput holds optional fields for PUT. Empty strings / nil leave values unchanged.
type UpdateInput struct {
	Name      *string
	Provider  *string
	Transport *string
	RoomID    *string
	KeyHex    *string
	Comment   *string
	Enabled   *bool
	Status    *string
}

// Update patches an instance and persists.
func (s *Store) Update(id string, in UpdateInput) (*Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	idx := -1
	for i := range st.Instances {
		if st.Instances[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, ErrNotFound
	}
	inst := st.Instances[idx]

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrInvalid)
		}
		inst.Name = name
	}
	if in.Provider != nil {
		inst.Provider = strings.TrimSpace(*in.Provider)
	}
	if in.Transport != nil {
		inst.Transport = strings.TrimSpace(*in.Transport)
	}
	if in.RoomID != nil {
		room := strings.TrimSpace(*in.RoomID)
		if room == "" {
			return nil, fmt.Errorf("%w: room_id is required", ErrInvalid)
		}
		inst.RoomID = room
	}
	if in.KeyHex != nil {
		inst.KeyHex = strings.TrimSpace(*in.KeyHex)
	}
	if in.Comment != nil {
		inst.Comment = strings.TrimSpace(*in.Comment)
	}
	if in.Enabled != nil {
		inst.Enabled = *in.Enabled
	}
	if in.Status != nil {
		stt := strings.TrimSpace(*in.Status)
		if stt != StatusRunning && stt != StatusStopped {
			return nil, fmt.Errorf("%w: status must be running or stopped", ErrInvalid)
		}
		inst.Status = stt
	}
	if err := ValidateInstanceFields(inst.Provider, inst.Transport, inst.KeyHex); err != nil {
		return nil, err
	}
	inst.UpdatedAt = time.Now().Unix()
	st.Instances[idx] = inst
	if err := s.saveUnlocked(st); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Delete removes an instance. Returns ErrNotFound if missing.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	next := make([]Instance, 0, len(st.Instances))
	found := false
	for _, existing := range st.Instances {
		if existing.ID == id {
			found = true
			continue
		}
		next = append(next, existing)
	}
	if !found {
		return ErrNotFound
	}
	st.Instances = next
	return s.saveUnlocked(st)
}

// Restart marks an instance as running (MVP stub — does not exec olcrtc).
func (s *Store) Restart(id string) (*Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	for i := range st.Instances {
		if st.Instances[i].ID == id {
			st.Instances[i].Status = StatusRunning
			st.Instances[i].UpdatedAt = time.Now().Unix()
			if err := s.saveUnlocked(st); err != nil {
				return nil, err
			}
			inst := st.Instances[i]
			return &inst, nil
		}
	}
	return nil, ErrNotFound
}

// MarkDeployed sets deployed=true (idempotent stub for future install).
func (s *Store) MarkDeployed() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.loadUnlocked()
	if err != nil {
		return false, err
	}
	st.Deployed = true
	if err := s.saveUnlocked(st); err != nil {
		return false, err
	}
	return true, nil
}

// Deployed reports whether the node has been marked deployed.
func (s *Store) Deployed() (bool, error) {
	st, err := s.Load()
	if err != nil {
		return false, err
	}
	return st.Deployed, nil
}

// ValidateInstanceFields checks provider, transport, and key_hex.
func ValidateInstanceFields(provider, transport, keyHex string) error {
	provider = strings.TrimSpace(provider)
	transport = strings.TrimSpace(transport)
	keyHex = strings.TrimSpace(keyHex)
	if !ValidProvider(provider) {
		return fmt.Errorf("%w: provider must be jitsi|telemost|wbstream", ErrInvalid)
	}
	if !ValidTransport(transport) {
		return fmt.Errorf("%w: transport must be datachannel|vp8channel|seichannel|videochannel", ErrInvalid)
	}
	if !CompatiblePair(provider, transport) {
		return fmt.Errorf("%w: transport incompatible with provider", ErrInvalid)
	}
	if err := ValidateKeyHex(keyHex); err != nil {
		return err
	}
	return nil
}

// CompatiblePair reports whether provider+transport is allowed.
func CompatiblePair(provider, transport string) bool {
	switch strings.TrimSpace(provider) {
	case "jitsi", "wbstream":
		return ValidTransport(transport)
	case "telemost":
		t := strings.TrimSpace(transport)
		return t == "vp8channel" || t == "videochannel"
	default:
		return false
	}
}

// ValidProvider reports whether provider is supported.
func ValidProvider(p string) bool {
	switch p {
	case "jitsi", "telemost", "wbstream":
		return true
	default:
		return false
	}
}

// ValidTransport reports whether transport is supported.
func ValidTransport(t string) bool {
	switch t {
	case "datachannel", "vp8channel", "seichannel", "videochannel":
		return true
	default:
		return false
	}
}

// ValidateKeyHex checks MVP crypto key length (32 bytes = 64 hex).
func ValidateKeyHex(key string) error {
	key = strings.TrimSpace(key)
	if len(key) != 64 {
		return fmt.Errorf("%w: key_hex must be 64 hex characters", ErrInvalid)
	}
	for _, c := range key {
		ok := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !ok {
			return fmt.Errorf("%w: key_hex must be hex", ErrInvalid)
		}
	}
	return nil
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to time-based hex so create still works.
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	}
	// UUID v4 bits.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

func randomKeyHex() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
