package protect

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/store"
)

// Current returns persisted protect profile (defaults if unset).
func Current(st *store.Store) (Profile, error) {
	state, err := st.Load()
	if err != nil {
		return Profile{}, err
	}
	if state.Protect == nil {
		return Default(), nil
	}
	return Normalize(*state.Protect)
}

// Apply writes base.cfg from entrances + protect profile and reloads HAProxy.
func Apply(ctx context.Context, st *store.Store, backendsDir string, docker *dockerctl.Controller, ha *haproxy.Client, want Profile) (Profile, error) {
	p, err := Normalize(want)
	if err != nil {
		return Profile{}, err
	}
	state, err := st.Load()
	if err != nil {
		return Profile{}, err
	}
	ents := state.Entrances
	if len(ents) == 0 {
		ents = []store.Entrance{store.DefaultEntrance}
	}

	body := haproxy.BaseConfigBody(ents, ToHAProxy(p))
	path := filepath.Join(backendsDir, haproxy.BaseConfigFile)
	if err := haproxy.AtomicWriteFile(path, body); err != nil {
		return Profile{}, fmt.Errorf("write base cfg: %w", err)
	}
	if docker != nil {
		if err := docker.Reload(ctx); err != nil {
			return Profile{}, fmt.Errorf("reload: %w", err)
		}
		if ha != nil {
			_ = ha.WaitReadyTimeout(ctx, 8*time.Second)
		}
	}
	cp := p
	state.Protect = &cp
	if err := st.Save(state); err != nil {
		return Profile{}, fmt.Errorf("save state: %w", err)
	}
	return p, nil
}

// EnsurePersistedDefaults writes Default() into state if Protect is missing.
func EnsurePersistedDefaults(st *store.Store) (Profile, error) {
	state, err := st.Load()
	if err != nil {
		return Profile{}, err
	}
	if state.Protect != nil {
		return Normalize(*state.Protect)
	}
	p := Default()
	state.Protect = &p
	if err := st.Save(state); err != nil {
		return p, err
	}
	return p, nil
}
