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

// EnsureRuntimeTCP makes sure HAProxy answers on the TCP runtime API.
// If the address already works (e.g. stats socket is in the main cfg), it does
// nothing — writing a second stats socket into backends.d causes bind conflicts.
func EnsureRuntimeTCP(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client) error {
	if ha == nil {
		return fmt.Errorf("haproxy client is nil")
	}
	ha.Addr = RuntimeTCPAddr

	probe := *ha
	probe.Timeout = 800 * time.Millisecond
	if raw, err := probe.Exec("show info"); err == nil && strings.Contains(raw, "Name:") {
		return nil
	}

	if backendsDir == "" {
		return fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(backendsDir, 0o755); err != nil {
		return err
	}

	// Base config already declares the TCP stats socket — do not add a second one.
	basePath := filepath.Join(backendsDir, BaseConfigFile)
	if raw, err := os.ReadFile(basePath); err == nil && strings.Contains(string(raw), "0.0.0.0:9999") {
		_ = os.Remove(filepath.Join(backendsDir, RuntimeTCPFile))
		if docker != nil {
			_ = docker.Reload(ctx)
			if err := ha.WaitReadyTimeout(ctx, 8*time.Second); err == nil {
				return nil
			}
			if err := docker.Restart(ctx); err != nil {
				return fmt.Errorf("restart haproxy for TCP runtime: %w", err)
			}
			if err := ha.WaitReadyTimeout(ctx, 20*time.Second); err != nil {
				return fmt.Errorf("haproxy TCP runtime not ready: %w", err)
			}
		}
		return nil
	}

	path := filepath.Join(backendsDir, RuntimeTCPFile)
	prev, _ := os.ReadFile(path)
	if string(prev) != runtimeTCPBody {
		if err := atomicWrite(path, runtimeTCPBody); err != nil {
			return err
		}
	}

	if docker == nil {
		return fmt.Errorf("haproxy TCP runtime not ready and no docker controller")
	}

	_ = docker.Reload(ctx)
	if err := ha.WaitReadyTimeout(ctx, 8*time.Second); err == nil {
		return nil
	}

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
