package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/azabash/hapanel/agent/internal/auth"
	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/store"
	"github.com/azabash/hapanel/agent/internal/sysinfo"
)

// Version is injected at build time via -ldflags.
var Version = "0.1.0"

// Deps holds handler dependencies.
type Deps struct {
	Log            *slog.Logger
	Auth           auth.Bearer
	HA             *haproxy.Client
	Cfg            *haproxy.ConfigWriter
	Store          *store.Store
	Docker         *dockerctl.Controller
	DefaultBackend string
	BackendsDir    string
}

// NewRouter builds the HTTP router under /_hapctl/v1.
func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.DefaultBackend == "" {
		d.DefaultBackend = "app"
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestLogger(d.Log))

	r.Route("/_hapctl/v1", func(r chi.Router) {
		r.Get("/health", d.handleHealth)

		r.Group(func(r chi.Router) {
			r.Use(d.Auth.Middleware)
			r.Get("/stats", d.handleStats)
			r.Get("/system", d.handleSystem)
			r.Get("/backends", d.handleListBackends)
			r.Post("/backends", d.handleAddBackend)
			r.Delete("/backends/{backend}/{name}", d.handleDeleteBackend)
			r.Post("/haproxy/reload", d.handleReload)
			r.Post("/haproxy/restart", d.handleRestart)
		})
	})

	return r
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (d Deps) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": Version,
	})
}

func (d Deps) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := d.HA.GetStats()
	if err != nil && d.Docker != nil && d.BackendsDir != "" {
		if heal := haproxy.EnsureRuntimeTCP(r.Context(), d.BackendsDir, d.Docker, d.HA); heal != nil {
			d.Log.Warn("runtime heal", "err", heal)
		} else {
			stats, err = d.HA.GetStats()
		}
	}
	if err != nil {
		d.Log.Error("stats", "err", err)
		writeErr(w, http.StatusBadGateway, "failed to read haproxy stats: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (d Deps) handleSystem(w http.ResponseWriter, _ *http.Request) {
	m, err := sysinfo.Collect()
	if err != nil {
		d.Log.Error("system metrics", "err", err)
		writeErr(w, http.StatusInternalServerError, "failed to collect system metrics: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type backendsResponse struct {
	Backends []backendGroup `json:"backends"`
}

type backendGroup struct {
	Name    string               `json:"name"`
	Servers []haproxy.ServerInfo `json:"servers"`
}

func (d Deps) handleListBackends(w http.ResponseWriter, _ *http.Request) {
	stored, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Prefer persisted address (what the operator entered). HAProxy runtime
	// "show servers state" always reports the resolved IP in srv_addr, which
	// made the UI look like hostnames were rewritten to IPs.
	statusByKey := map[string]string{}
	if live, lerr := d.HA.ListServers(); lerr == nil {
		for _, s := range live {
			statusByKey[s.Backend+"/"+s.Name] = s.Status
		}
	} else {
		d.Log.Warn("list servers via runtime failed, status unknown", "err", lerr)
	}

	servers := make([]haproxy.ServerInfo, 0, len(stored))
	for _, s := range stored {
		st := statusByKey[s.Backend+"/"+s.Name]
		if st == "" {
			st = "unknown"
		}
		servers = append(servers, haproxy.ServerInfo{
			Backend: s.Backend,
			Name:    s.Name,
			Address: s.Address,
			Port:    s.Port,
			Weight:  s.Weight,
			Status:  st,
		})
	}

	byBackend := map[string][]haproxy.ServerInfo{}
	order := []string{}
	for _, s := range servers {
		if _, ok := byBackend[s.Backend]; !ok {
			order = append(order, s.Backend)
		}
		byBackend[s.Backend] = append(byBackend[s.Backend], s)
	}

	out := backendsResponse{Backends: make([]backendGroup, 0, len(order))}
	for _, name := range order {
		out.Backends = append(out.Backends, backendGroup{
			Name:    name,
			Servers: byBackend[name],
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type addBackendRequest struct {
	Backend string `json:"backend"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Weight  int    `json:"weight"`
}

func (d Deps) handleAddBackend(w http.ResponseWriter, r *http.Request) {
	var req addBackendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Backend == "" {
		req.Backend = d.DefaultBackend
	}
	if req.Name == "" || req.Address == "" || req.Port <= 0 || req.Port > 65535 {
		writeErr(w, http.StatusBadRequest, "name, address and valid port are required")
		return
	}
	if req.Weight <= 0 {
		req.Weight = 100
	}

	srv := store.Server{
		Backend: req.Backend,
		Name:    req.Name,
		Address: req.Address,
		Port:    req.Port,
		Weight:  req.Weight,
	}
	if err := d.Store.Upsert(srv); err != nil {
		writeErr(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}

	all, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := d.Cfg.Write(all); err != nil {
		writeErr(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}

	// Best-effort runtime add before reload (may fail if dynamic servers unsupported).
	if err := d.HA.AddServerRuntime(req.Backend, req.Name, req.Address, req.Port, req.Weight); err != nil {
		d.Log.Info("runtime add skipped/failed, will reload", "err", err)
	}
	if err := d.Docker.Reload(r.Context()); err != nil {
		d.Log.Error("reload after add", "err", err)
		writeErr(w, http.StatusBadGateway, "config written but reload failed: "+err.Error())
		return
	}
	if err := d.waitHAReady(r.Context()); err != nil {
		d.Log.Error("haproxy not ready after reload", "err", err)
		writeErr(w, http.StatusBadGateway, "config written but haproxy not ready: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"ok":     true,
		"server": srv,
	})
}

func (d Deps) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	backend := chi.URLParam(r, "backend")
	name := chi.URLParam(r, "name")
	if backend == "" || name == "" {
		writeErr(w, http.StatusBadRequest, "backend and name are required")
		return
	}

	removed, err := d.Store.Delete(backend, name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeErr(w, http.StatusNotFound, "server not found in store")
		return
	}

	all, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := d.Cfg.Write(all); err != nil {
		writeErr(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}

	// Prefer disable then delete via runtime; ignore failures and reload.
	_ = d.HA.SetServerState(backend, name, "maint")
	if err := d.HA.DelServerRuntime(backend, name); err != nil {
		d.Log.Info("runtime del skipped/failed, will reload", "err", err)
	}
	if err := d.Docker.Reload(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "config written but reload failed: "+err.Error())
		return
	}
	if err := d.waitHAReady(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "config written but haproxy not ready: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": name, "backend": backend})
}

func (d Deps) handleReload(w http.ResponseWriter, r *http.Request) {
	// Ensure config is synced from store before reload.
	all, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := d.Cfg.Write(all); err != nil {
		writeErr(w, http.StatusInternalServerError, "write config: "+err.Error())
		return
	}
	if err := d.Docker.Reload(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := d.waitHAReady(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "reload issued but haproxy not ready: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "reload"})
}

func (d Deps) handleRestart(w http.ResponseWriter, r *http.Request) {
	if err := d.Docker.Restart(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := d.waitHAReady(r.Context()); err != nil {
		writeErr(w, http.StatusBadGateway, "restarted but haproxy not ready: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "restart"})
}

// waitHAReady blocks until the admin socket answers after restart/reload.
func (d Deps) waitHAReady(ctx context.Context) error {
	if d.HA == nil {
		return nil
	}
	return d.HA.WaitReady(ctx)
}
