package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/azabash/hapanel/agent/internal/api"
	"github.com/azabash/hapanel/agent/internal/auth"
	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := loadConfig()
	if cfg.Token == "" {
		log.Error("HAPANEL_TOKEN is required")
		os.Exit(1)
	}

	st := store.New(cfg.StatePath)
	socket := haproxy.ResolveSocket(cfg.HAProxySocket)
	ha := haproxy.NewClient(socket)
	cfgWriter := haproxy.NewConfigWriter(cfg.BackendsDir)

	// Write frontends into backends.d before HAProxy starts (compose depends_on agent).
	if _, err := haproxy.EnsureBaseConfig(context.Background(), cfg.BackendsDir, nil, nil); err != nil {
		log.Warn("base config write failed", "err", err)
	}

	// Sync persisted servers into backends.d on startup.
	servers, err := st.List()
	if err != nil {
		log.Error("load state", "err", err)
		os.Exit(1)
	}
	if err := cfgWriter.Write(servers); err != nil {
		log.Warn("initial config write failed", "err", err)
	}

	docker, err := dockerctl.New(cfg.DockerHost, cfg.HAProxyContainer)
	if err != nil {
		log.Error("docker client", "err", err)
		os.Exit(1)
	}
	defer docker.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Self-heal: write client frontends (:80/:8443) into backends.d (never rely
	// on a host bind-mounted haproxy.cfg — Docker turns a missing file into a dir).
	// Then ensure TCP runtime API for the panel.
	go func() {
		haproxy.EnsureBaseConfigLoop(ctx, cfg.BackendsDir, docker, ha, func(msg string, args ...any) {
			log.Info(msg, args...)
		})
		haproxy.EnsureRuntimeTCPLoop(ctx, cfg.BackendsDir, docker, ha, func(msg string, args ...any) {
			log.Info(msg, args...)
		})
	}()

	router := api.NewRouter(api.Deps{
		Log:            log,
		Auth:           auth.Bearer{Token: cfg.Token},
		HA:             ha,
		Cfg:            cfgWriter,
		Store:          st,
		Docker:         docker,
		DefaultBackend: cfg.DefaultBackend,
		BackendsDir:    cfg.BackendsDir,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("agent listening",
			"addr", cfg.ListenAddr,
			"version", api.Version,
			"socket", socket,
			"container", cfg.HAProxyContainer,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	log.Info("bye")
}

type config struct {
	ListenAddr       string
	Token            string
	HAProxySocket    string
	BackendsDir      string
	StatePath        string
	DockerHost       string
	HAProxyContainer string
	DefaultBackend   string
}

func loadConfig() config {
	return config{
		ListenAddr:       getenv("HAPANEL_LISTEN", "0.0.0.0:9100"),
		Token:            os.Getenv("HAPANEL_TOKEN"),
		// Prefer TCP runtime API on the docker network (avoids flaky unix socket volumes).
		HAProxySocket: getenv("HAPROXY_SOCKET", "tcp://haproxy:9999"),
		BackendsDir:      getenv("HAPROXY_BACKENDS_DIR", "/etc/haproxy/backends.d"),
		StatePath:        getenv("HAPANEL_STATE_PATH", "/var/lib/hapanel/state.json"),
		DockerHost:       getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		HAProxyContainer: getenv("HAPROXY_CONTAINER", "haproxy"),
		DefaultBackend:   getenv("HAPANEL_DEFAULT_BACKEND", "app"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
