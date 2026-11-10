package haproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/store"
)

// PanelApplyRequest is the panel → agent apply payload.
type PanelApplyRequest struct {
	ProfileID string
	Revision  int
	Engine    string
	Body      string
}

// PanelApplyResult is returned after a successful (or dry) apply.
type PanelApplyResult struct {
	ProfileID       string `json:"profile_id"`
	Revision        int    `json:"revision"`
	Engine          string `json:"engine"`
	BackendsKept    int    `json:"backends_kept"`
	BackendsStripped int   `json:"backends_stripped"`
	AppliedAt       string `json:"applied_at"`
	Message         string `json:"message"`
}

// ApplyPanelProfile writes the panel base profile, regenerates backend fragments
// from agent state (so servers do not disappear), validates, reloads.
func ApplyPanelProfile(
	ctx context.Context,
	st *store.Store,
	backendsDir string,
	cfg *ConfigWriter,
	docker *dockerctl.Controller,
	ha *Client,
	req PanelApplyRequest,
) (PanelApplyResult, error) {
	engine := req.Engine
	if engine == "" {
		engine = "haproxy"
	}
	if engine != "haproxy" {
		return PanelApplyResult{}, fmt.Errorf("движок %q пока не поддерживается агентом", engine)
	}
	base, stripped := StripBackendSections(req.Body)
	if err := ValidateBaseProfile(base); err != nil {
		return PanelApplyResult{}, err
	}

	state, err := st.Load()
	if err != nil {
		return PanelApplyResult{}, err
	}
	servers := state.Servers
	balances := state.Balances
	if balances == nil {
		balances = map[string]string{}
	}
	ensure := make([]string, 0, len(state.Entrances)+1)
	ensure = append(ensure, "app")
	for _, e := range state.Entrances {
		ensure = append(ensure, e.Backend)
	}

	if backendsDir == "" {
		return PanelApplyResult{}, fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(backendsDir, 0o755); err != nil {
		return PanelApplyResult{}, err
	}
	basePath := filepath.Join(backendsDir, BaseConfigFile)
	prevBase, _ := os.ReadFile(basePath)

	if err := AtomicWriteFile(basePath, base); err != nil {
		return PanelApplyResult{}, fmt.Errorf("write base: %w", err)
	}
	if cfg != nil {
		if err := cfg.Write(servers, balances, ensure...); err != nil {
			_ = AtomicWriteFile(basePath, string(prevBase))
			return PanelApplyResult{}, fmt.Errorf("rewrite backends: %w", err)
		}
	}

	if docker != nil {
		if out, err := docker.Exec(ctx, []string{"haproxy", "-c", "-f", "/etc/haproxy/backends.d"}); err != nil {
			_ = AtomicWriteFile(basePath, string(prevBase))
			if cfg != nil {
				_ = cfg.Write(servers, balances, ensure...)
			}
			return PanelApplyResult{}, fmt.Errorf("haproxy -c failed: %w (%s)", err, truncateOut(out, 800))
		}
		if err := docker.Reload(ctx); err != nil {
			_ = AtomicWriteFile(basePath, string(prevBase))
			if cfg != nil {
				_ = cfg.Write(servers, balances, ensure...)
			}
			_ = docker.Reload(ctx)
			return PanelApplyResult{}, fmt.Errorf("reload: %w", err)
		}
		if ha != nil {
			if err := ha.WaitReadyTimeout(ctx, 12*time.Second); err != nil {
				return PanelApplyResult{}, fmt.Errorf("haproxy not ready after apply: %w", err)
			}
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	state.PanelProxy = &store.PanelProxy{
		ProfileID: req.ProfileID,
		Revision:  req.Revision,
		Engine:    engine,
		Body:      base,
		AppliedAt: now,
	}
	if err := st.Save(state); err != nil {
		return PanelApplyResult{}, fmt.Errorf("save state: %w", err)
	}

	return PanelApplyResult{
		ProfileID:        req.ProfileID,
		Revision:         req.Revision,
		Engine:           engine,
		BackendsKept:     len(servers),
		BackendsStripped: stripped,
		AppliedAt:        now,
		Message:          "профиль применён; backend-серверы сохранены из state",
	}, nil
}

// EnsurePanelBase restores the panel-managed base file if present in state.
// Returns true when panel profile is active (caller should skip generated base).
func EnsurePanelBase(ctx context.Context, st *store.Store, backendsDir string, docker *dockerctl.Controller, ha *Client) (managed bool, changed bool, err error) {
	state, err := st.Load()
	if err != nil {
		return false, false, err
	}
	if state.PanelProxy == nil || strings.TrimSpace(state.PanelProxy.Body) == "" {
		return false, false, nil
	}
	path := filepath.Join(backendsDir, BaseConfigFile)
	prev, _ := os.ReadFile(path)
	body := state.PanelProxy.Body
	if string(prev) == body {
		return true, false, nil
	}
	if err := AtomicWriteFile(path, body); err != nil {
		return true, false, err
	}
	if docker != nil {
		_ = docker.Reload(ctx)
		if ha != nil {
			_ = ha.WaitReadyTimeout(ctx, 8*time.Second)
		}
	}
	return true, true, nil
}

func truncateOut(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
