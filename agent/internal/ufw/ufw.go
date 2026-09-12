package ufw

import (
	"context"
	"fmt"
	"strings"

	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/ports"
)

// Sync opens newly added listen ports and deletes rules for removed ones.
// No-op when ufw is not installed. Never touches SSH or other unrelated rules
// outside the old→new listen-port diff.
func Sync(ctx context.Context, docker *dockerctl.Controller, oldPorts, newPorts []int) (opened, closed []int, err error) {
	if docker == nil {
		return nil, nil, fmt.Errorf("docker controller is nil")
	}
	open, closePorts := ports.Diff(oldPorts, newPorts)
	if len(open) == 0 && len(closePorts) == 0 {
		return nil, nil, nil
	}

	check, err := docker.HostShell(ctx, `command -v ufw >/dev/null 2>&1 && echo yes || echo no`)
	if err != nil {
		return nil, nil, fmt.Errorf("ufw check: %w", err)
	}
	if !strings.Contains(check, "yes") {
		return open, closePorts, nil
	}

	var cmds []string
	for _, p := range open {
		cmds = append(cmds, fmt.Sprintf(`ufw allow %d/tcp || true`, p))
	}
	for _, p := range closePorts {
		// Prefer numbered delete when possible; fall back to rule text.
		cmds = append(cmds, fmt.Sprintf(`ufw delete allow %d/tcp || ufw --force delete allow %d/tcp || true`, p, p))
	}
	script := "set -e\n" + strings.Join(cmds, "\n") + "\nufw status numbered 2>/dev/null | head -40 || ufw status || true\n"
	if _, err := docker.HostShell(ctx, script); err != nil {
		return open, closePorts, fmt.Errorf("ufw sync: %w", err)
	}
	return open, closePorts, nil
}
