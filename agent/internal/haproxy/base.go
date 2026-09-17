package haproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/store"
)

// BaseConfigFile holds global/defaults/frontends. Loaded from backends.d so we
// never bind-mount a single host file (Docker creates a directory if missing).
const BaseConfigFile = "00-hapanel-base.cfg"

// nbthreadCount returns HAProxy worker threads: HAPROXY_NBTHREAD env, else
// host/container CPU count (capped). Oversized nbthread on tiny VPS wastes CPU.
func nbthreadCount() int {
	if v := strings.TrimSpace(os.Getenv("HAPROXY_NBTHREAD")); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			if n > 64 {
				return 64
			}
			return n
		}
	}
	n := runtime.NumCPU()
	if n < 1 {
		return 1
	}
	if n > 64 {
		return 64
	}
	return n
}

// FrontendName returns the HAProxy frontend section name for an entrance port.
func FrontendName(port int) string {
	return fmt.Sprintf("entrance_%d", port)
}

// BaseConfigBody returns the canonical HAProxy frontends for client traffic.
// Each entrance is its own frontend (one bind + default_backend).
func BaseConfigBody(entrances []store.Entrance, protect ProtectOpts) string {
	ents, err := store.NormalizeEntrances(entrances)
	if err != nil || len(ents) == 0 {
		ents = []store.Entrance{store.DefaultEntrance}
	}
	extras := protect.frontendExtras()
	var fronts strings.Builder
	for _, e := range ents {
		fmt.Fprintf(&fronts, `
frontend %s
    mode tcp
    bind *:%d
    maxconn 40000
%s    default_backend %s
`, FrontendName(e.Port), e.Port, extras, e.Backend)
	}
	return fmt.Sprintf(`# Managed by hapanel agent — do not edit by hand
# Client frontends live here (not a host bind-mounted haproxy.cfg).
global
    maxconn 50000
    nbthread %d
    hard-stop-after 5m
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
    master-worker

defaults
    mode    tcp
    no log
    option  splice-auto
    timeout connect 5s
    timeout client  30m
    timeout server  30m
    timeout tunnel  30m
    timeout client-fin 30s
    timeout server-fin 30s
    retries 2
%s`, nbthreadCount(), fronts.String())
}

// AtomicWriteFile writes body to path atomically (exported for listen-ports apply).
func AtomicWriteFile(path, body string) error {
	return atomicWrite(path, body)
}

// EnsureBaseConfig writes frontends into backends.d and reloads HAProxy when needed.
func EnsureBaseConfig(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client, entrances []store.Entrance, protect ProtectOpts) (changed bool, err error) {
	if backendsDir == "" {
		return false, fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(backendsDir, 0o755); err != nil {
		return false, err
	}
	path := filepath.Join(backendsDir, BaseConfigFile)
	body := BaseConfigBody(entrances, protect)
	prev, _ := os.ReadFile(path)
	if string(prev) == body {
		return false, nil
	}
	if err := atomicWrite(path, body); err != nil {
		return false, err
	}
	if docker == nil {
		return true, nil
	}
	_ = docker.Reload(ctx)
	if ha != nil {
		_ = ha.WaitReadyTimeout(ctx, 8*time.Second)
	}
	return true, nil
}

// EnsureBaseConfigLoop keeps base frontends present (fixes empty/missing mounts).
func EnsureBaseConfigLoop(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client, entrances []store.Entrance, protect ProtectOpts, log func(msg string, args ...any)) {
	backoff := time.Second
	ports := store.PortsFromEntrances(entrances)
	for {
		if ctx.Err() != nil {
			return
		}
		changed, err := EnsureBaseConfig(ctx, backendsDir, docker, ha, entrances, protect)
		if err == nil {
			if log != nil {
				if changed {
					log("haproxy base config applied", "file", BaseConfigFile, "ports", ports)
				} else {
					log("haproxy base config ok", "file", BaseConfigFile)
				}
			}
			return
		}
		if log != nil {
			log("waiting to apply haproxy base config", "err", err)
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
