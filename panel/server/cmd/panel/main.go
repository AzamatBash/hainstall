package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/api"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/config"
	"github.com/azabash/hapanel/panel/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		logger.Error("create db dir", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	au := auth.New(cfg.PanelPassword, cfg.JWTSecret, cfg.SessionTTL)
	ag := agent.NewWithHello(cfg.InsecureSkipVerify, cfg.UTLSHello)
	apiSrv := api.New(st, au, ag, logger)

	mux := http.NewServeMux()
	mux.Handle("/api/", apiSrv.Handler())
	mux.Handle("/", spaHandler(cfg.StaticDir, logger))

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("panel listening",
			"addr", cfg.ListenAddr,
			"db", cfg.DBPath,
			"static", cfg.StaticDir,
			"insecure_skip_verify", cfg.InsecureSkipVerify,
			"utls_hello", ag.HelloName(),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
		os.Exit(1)
	}
}

// spaHandler serves built UI files and falls back to index.html for client routes.
// Important: do not pass directory paths to http.FileServer — with Go 1.22+ ServeMux
// stripping the "/" prefix, that produces 301 Location: ./ and Firefox redirect loops
// on /login and /nodes/:id refresh.
func spaHandler(staticDir string, logger *slog.Logger) http.Handler {
	indexPath := filepath.Join(staticDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		p = path.Clean(p)
		if p == "/" || p == "/." {
			http.ServeFile(w, r, indexPath)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			http.NotFound(w, r)
			return
		}

		rel := strings.TrimPrefix(p, "/")
		full := filepath.Join(staticDir, filepath.FromSlash(rel))
		// Prevent path escape outside staticDir.
		if !strings.HasPrefix(full, filepath.Clean(staticDir)+string(os.PathSeparator)) &&
			filepath.Clean(full) != filepath.Clean(staticDir) {
			http.NotFound(w, r)
			return
		}
		fi, err := os.Stat(full)
		if err == nil && fi.Mode().IsRegular() {
			http.ServeFile(w, r, full)
			return
		}

		if _, err := os.Stat(indexPath); err != nil {
			logger.Debug("static missing", "path", p, "err", err)
			http.Error(w, "UI not built — run npm run build in panel/web", http.StatusNotFound)
			return
		}
		http.ServeFile(w, r, indexPath)
	})
}
