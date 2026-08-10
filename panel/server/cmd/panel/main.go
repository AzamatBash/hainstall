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
	"github.com/azabash/hapanel/panel/internal/remnastats"
	"github.com/azabash/hapanel/panel/internal/snapshot"
	"github.com/azabash/hapanel/panel/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := config.Load()

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		logger.Error("create db dir", "err", err)
		os.Exit(1)
	}

	imagesDir := filepath.Join(filepath.Dir(cfg.DBPath), "task-images")
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		logger.Error("create task images dir", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	if os.Getenv("PANEL_OLCRTC_SEED") == "1" {
		// Seed is a no-op: use POST /api/olcrtc/nodes/deploy-local instead.
		if _, err := st.SeedOlcrtcDemo(); err != nil {
			logger.Error("olcrtc demo seed", "err", err)
			os.Exit(1)
		}
		logger.Info("olcrtc seed skipped — use deploy-local")
	}

	cookiePath := "/"
	if cfg.BasePath != "" {
		cookiePath = cfg.BasePath
	}
	au := auth.NewWithCookie(cfg.PanelPassword, cfg.JWTSecret, cfg.SessionTTL, cookiePath, cfg.BasePath != "")
	ag := agent.NewWithHello(cfg.InsecureSkipVerify, cfg.UTLSHello)
	secretsKey := cfg.SecretsKey
	if secretsKey == "" {
		secretsKey = cfg.JWTSecret
	}
	remnaStats := remnastats.New(st, secretsKey, logger)

	apiSrv := api.New(st, au, ag, logger, api.Options{
		SecretsKey:         secretsKey,
		GeminiKey:          cfg.GeminiAPIKey,
		GroqKey:            cfg.GroqAPIKey,
		LLMProvider:        cfg.LLMProvider,
		PanelIP:            cfg.PanelIP,
		LLMProxy:           cfg.LLMHTTPProxy,
		ImagesDir:          imagesDir,
		RemnaStats:         remnaStats,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
	})

	mux := http.NewServeMux()
	mux.Handle("/api/", apiSrv.Handler())
	mux.Handle("/", spaHandler(cfg.StaticDir, cfg.BasePath, logger))

	var handler http.Handler = mux
	if cfg.BasePath != "" {
		handler = basePathMiddleware(cfg.BasePath, mux)
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	snapshot.Start(ctx, st, ag, logger)
	remnaStats.Start(ctx)

	go func() {
		logger.Info("panel listening",
			"addr", cfg.ListenAddr,
			"db", cfg.DBPath,
			"static", cfg.StaticDir,
			"base_path", cfg.BasePath,
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

func basePathMiddleware(base string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == base || p == base+"/" {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			next.ServeHTTP(w, r2)
			return
		}
		prefix := base + "/"
		if strings.HasPrefix(p, prefix) {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + strings.TrimPrefix(p, prefix)
			next.ServeHTTP(w, r2)
			return
		}
		// Outside secret path — look like a blank host.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>404</title></head><body>Not Found</body></html>`))
	})
}

// spaHandler serves built UI files and falls back to index.html for client routes.
// Important: do not pass directory paths to http.FileServer — with Go 1.22+ ServeMux
// stripping the "/" prefix, that produces 301 Location: ./ and Firefox redirect loops
// on /login and /nodes/:id refresh.
func spaHandler(staticDir, basePath string, logger *slog.Logger) http.Handler {
	indexPath := filepath.Join(staticDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		p = path.Clean(p)
		if p == "/" || p == "/." {
			serveIndex(w, indexPath, basePath, logger)
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
		serveIndex(w, indexPath, basePath, logger)
	})
}

func serveIndex(w http.ResponseWriter, indexPath, basePath string, logger *slog.Logger) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		logger.Error("read index", "err", err)
		http.Error(w, "UI missing", http.StatusNotFound)
		return
	}
	html := string(raw)
	if basePath != "" {
		inject := `<script>window.__HAPANEL_BASE__="` + basePath + `";</script>`
		if strings.Contains(html, "<head>") {
			html = strings.Replace(html, "<head>", "<head>"+inject, 1)
		} else {
			html = inject + html
		}
		// Vite emits absolute /assets/... — rewrite for secret path.
		html = strings.ReplaceAll(html, `href="/assets/`, `href="`+basePath+`/assets/`)
		html = strings.ReplaceAll(html, `src="/assets/`, `src="`+basePath+`/assets/`)
		html = strings.ReplaceAll(html, `href="/vite.svg"`, `href="`+basePath+`/vite.svg"`)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}
