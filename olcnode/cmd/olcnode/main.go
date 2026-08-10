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

	"github.com/azabash/hapanel/olcnode/internal/api"
	"github.com/azabash/hapanel/olcnode/internal/auth"
	"github.com/azabash/hapanel/olcnode/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := loadConfig()
	if cfg.Token == "" {
		log.Error("OLCNODE_TOKEN is required (legacy: OLCRTC_AGENT_TOKEN)")
		os.Exit(1)
	}

	st := store.New(cfg.StatePath)
	started := time.Now()

	router := api.NewRouter(api.Deps{
		Log:      log,
		Auth:     auth.Bearer{Token: cfg.Token},
		Store:    st,
		NodeName: cfg.NodeName,
		Started:  started,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("olcnode listening",
			"addr", cfg.ListenAddr,
			"version", api.Version,
			"state", cfg.StatePath,
			"node_name", cfg.NodeName,
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
	ListenAddr string
	Token      string
	StatePath  string
	NodeName   string
}

func loadConfig() config {
	return config{
		ListenAddr: env2("OLCNODE_LISTEN", "OLCRTC_AGENT_LISTEN", ":9200"),
		Token:      env2("OLCNODE_TOKEN", "OLCRTC_AGENT_TOKEN", ""),
		StatePath:  env2("OLCNODE_STATE", "OLCRTC_AGENT_STATE", "./data/state.json"),
		NodeName:   env2("OLCNODE_NAME", "OLCRTC_AGENT_NODE_NAME", ""),
	}
}

func env2(primary, legacy, def string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if v := os.Getenv(legacy); v != "" {
		return v
	}
	return def
}
