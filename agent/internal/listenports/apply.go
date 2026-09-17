package listenports

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/azabash/hapanel/agent/internal/composeedit"
	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/ports"
	"github.com/azabash/hapanel/agent/internal/protect"
	"github.com/azabash/hapanel/agent/internal/store"
	"github.com/azabash/hapanel/agent/internal/ufw"
)

// ApplyEntrances updates HAProxy frontends, docker publish ports, UFW, and state.
func ApplyEntrances(ctx context.Context, st *store.Store, backendsDir string, docker *dockerctl.Controller, ha *haproxy.Client, want []store.Entrance) (ents []store.Entrance, opened, closed []int, err error) {
	ents, err = store.NormalizeEntrances(want)
	if err != nil {
		return nil, nil, nil, err
	}
	newPorts := store.PortsFromEntrances(ents)

	state, err := st.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	oldPorts := store.PortsFromEntrances(state.Entrances)
	if len(oldPorts) == 0 {
		oldPorts = append([]int(nil), ports.DefaultListen...)
	}

	same := len(oldPorts) == len(newPorts)
	if same {
		for i := range oldPorts {
			if oldPorts[i] != newPorts[i] {
				same = false
				break
			}
		}
	}

	prot := protect.Default()
	if state.Protect != nil {
		if p, err := protect.Normalize(*state.Protect); err == nil {
			prot = p
		}
	}

	body := haproxy.BaseConfigBody(ents, protect.ToHAProxy(prot))
	path := filepath.Join(backendsDir, haproxy.BaseConfigFile)
	if err := haproxy.AtomicWriteFile(path, body); err != nil {
		return nil, nil, nil, fmt.Errorf("write base cfg: %w", err)
	}

	opened, closed, err = ufw.Sync(ctx, docker, oldPorts, newPorts)
	if err != nil {
		return nil, nil, nil, err
	}

	if docker != nil && !same {
		if err := republishHAProxy(ctx, docker, newPorts); err != nil {
			return nil, nil, nil, err
		}
		if ha != nil {
			_ = ha.WaitReadyTimeout(ctx, 15*time.Second)
		}
	} else if docker != nil {
		_ = docker.Reload(ctx)
		if ha != nil {
			_ = ha.WaitReadyTimeout(ctx, 8*time.Second)
		}
	}

	state.Entrances = ents
	state.ListenPorts = newPorts
	if err := st.Save(state); err != nil {
		return nil, nil, nil, fmt.Errorf("save state: %w", err)
	}
	return ents, opened, closed, nil
}

// Apply updates entrances from a flat port list (preserves backends for kept ports).
func Apply(ctx context.Context, st *store.Store, backendsDir string, docker *dockerctl.Controller, ha *haproxy.Client, want []int) (newPorts, opened, closed []int, err error) {
	newPorts, err = ports.Normalize(want)
	if err != nil {
		return nil, nil, nil, err
	}
	state, err := st.Load()
	if err != nil {
		return nil, nil, nil, err
	}
	ents := store.EntrancesFromPorts(newPorts, state.Entrances, "app")
	applied, opened, closed, err := ApplyEntrances(ctx, st, backendsDir, docker, ha, ents)
	if err != nil {
		return nil, nil, nil, err
	}
	return store.PortsFromEntrances(applied), opened, closed, nil
}

func republishHAProxy(ctx context.Context, docker *dockerctl.Controller, listenPorts []int) error {
	dir, err := docker.ComposeProjectDir(ctx)
	if err != nil {
		return err
	}
	composePath := filepath.Join(dir, "docker-compose.yml")
	raw, err := docker.ReadHostFile(ctx, composePath)
	if err != nil {
		// fallback common name
		composePath = filepath.Join(dir, "compose.yml")
		raw, err = docker.ReadHostFile(ctx, composePath)
		if err != nil {
			return fmt.Errorf("read compose: %w", err)
		}
	}
	updated, err := composeedit.SetHAProxyPublishPorts(string(raw), listenPorts)
	if err != nil {
		return err
	}
	if updated != string(raw) {
		if err := docker.WriteHostFile(ctx, composePath, []byte(updated)); err != nil {
			return fmt.Errorf("write compose: %w", err)
		}
	}
	script := fmt.Sprintf(`set -e
cd %q
docker compose up -d --force-recreate haproxy
`, dir)
	out, err := docker.HostShell(ctx, script)
	if err != nil {
		return fmt.Errorf("compose recreate: %w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

// Current returns persisted listen ports (default 8443).
func Current(st *store.Store) ([]int, error) {
	ents, err := CurrentEntrances(st)
	if err != nil {
		return nil, err
	}
	return store.PortsFromEntrances(ents), nil
}

// CurrentEntrances returns persisted entrances.
func CurrentEntrances(st *store.Store) ([]store.Entrance, error) {
	return st.Entrances()
}
