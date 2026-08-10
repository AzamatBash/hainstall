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

// BaseConfigBody returns the canonical HAProxy frontends for client traffic.
func BaseConfigBody() string {
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

frontend http_plain
    mode http
    bind *:80
    acl is_acme path_beg /.well-known/acme-challenge/
    http-request redirect scheme https code 301 unless is_acme
    use_backend acme if is_acme

frontend https_front
    mode tcp
    bind *:8443
    maxconn 40000
    default_backend app

backend acme
    mode http
    server local 127.0.0.1:8080
`, nbthreadCount())
}

// EnsureBaseConfig writes frontends into backends.d and reloads HAProxy when needed.
func EnsureBaseConfig(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client) (changed bool, err error) {
	if backendsDir == "" {
		return false, fmt.Errorf("backends dir is empty")
	}
	if err := os.MkdirAll(backendsDir, 0o755); err != nil {
		return false, err
	}
	path := filepath.Join(backendsDir, BaseConfigFile)
	body := BaseConfigBody()
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
func EnsureBaseConfigLoop(ctx context.Context, backendsDir string, docker *dockerctl.Controller, ha *Client, log func(msg string, args ...any)) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		changed, err := EnsureBaseConfig(ctx, backendsDir, docker, ha)
		if err == nil {
			if log != nil {
				if changed {
					log("haproxy base config applied", "file", BaseConfigFile)
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
