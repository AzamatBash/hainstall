package haproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/azabash/hapanel/agent/internal/dockerctl"
)

// RuntimeTCPFile is loaded via -f backends.d and adds a TCP stats socket.
// Must not be deleted by ConfigWriter.
const RuntimeTCPFile = "00-hapanel-runtime.cfg"

const RuntimeTCPAddr = "tcp://haproxy:9999"

const runtimeTCPBody = `# Managed by hapanel agent — TCP runtime API for docker network
global
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
`

// ResolveSocket picks the runtime API address.
// Old compose often pins a unix path that never appears on the shared volume —
// those are ignored in favour of TCP on the docker network.
func ResolveSocket(env string) string {
	env = strings.TrimSpace(env)
	if env == "" {
		return RuntimeTCPAddr
	}
	switch {
	case strings.HasPrefix(env, "tcp://"):
		return env
	case strings.HasPrefix(env, "unix://"), strings.HasPrefix(env, "/"):
		return RuntimeTCPAddr
	case strings.Contains(env, ":"):
		return env
	default:
		return RuntimeTCPAddr
	}
}

// EnsureRuntimeTCP writes the TCP stats snippet into backends.d and makes sure
// HAProxy is listening on it (reload/restart via Docker if needed).
func EnsureRuntimeTCP(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client) error {
	if backendsDir == "" {
		return fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(backendsDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(backendsDir, RuntimeTCPFile)
	prev, _ := os.ReadFile(path)
	changed := string(prev) != runtimeTCPBody
	if changed {
		if err := atomicWrite(path, runtimeTCPBody); err != nil {
			return err
		}
	}

	ha.Addr = RuntimeTCPAddr

	// Fast path: already answering.
	probe := *ha
	probe.Timeout = 800 * time.Millisecond
	if raw, err := probe.Exec("show info"); err == nil && strings.Contains(raw, "Name:") {
		return nil
	}

	if docker == nil {
		return fmt.Errorf("haproxy TCP runtime not ready and no docker controller")
	}

	// Soft reload first (picks up new -f snippet without dropping traffic).
	_ = docker.Reload(ctx)
	if err := ha.WaitReadyTimeout(ctx, 8*time.Second); err == nil {
		return nil
	}

	// Binding a new stats socket often needs a full container restart.
	if err := docker.Restart(ctx); err != nil {
		return fmt.Errorf("restart haproxy for TCP runtime: %w", err)
	}
	if err := ha.WaitReadyTimeout(ctx, 20*time.Second); err != nil {
		return fmt.Errorf("haproxy TCP runtime not ready: %w", err)
	}
	return nil
}

// EnsureRuntimeTCPLoop retries until HAProxy is up (agent often starts first).
func EnsureRuntimeTCPLoop(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client, log func(msg string, args ...any)) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := EnsureRuntimeTCP(ctx, backendsDir, docker, ha)
		if err == nil {
			if log != nil {
				log("haproxy runtime ready", "addr", ha.Addr)
			}
			return
		}
		if log != nil {
			log("waiting for haproxy runtime", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}
