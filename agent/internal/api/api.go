package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/azabash/hapanel/agent/internal/auth"
	"github.com/azabash/hapanel/agent/internal/dockerctl"
	"github.com/azabash/hapanel/agent/internal/haproxy"
	"github.com/azabash/hapanel/agent/internal/listenports"
	"github.com/azabash/hapanel/agent/internal/protect"
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
			r.Get("/listen-ports", d.handleGetListenPorts)
			r.Put("/listen-ports", d.handlePutListenPorts)
			r.Get("/entrances", d.handleGetEntrances)
			r.Put("/entrances", d.handlePutEntrances)
			r.Get("/protect", d.handleGetProtect)
			r.Put("/protect", d.handlePutProtect)
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
	Balance string               `json:"balance"`
	Servers []haproxy.ServerInfo `json:"servers"`
}

func (d Deps) handleListBackends(w http.ResponseWriter, _ *http.Request) {
	stored, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	balances, err := d.Store.Balances()
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
	// Entrances may reference backends with no servers yet — still list them.
	if ents, eerr := d.Store.Entrances(); eerr == nil {
		for _, e := range ents {
			if e.Backend == "" {
				continue
			}
			if _, ok := byBackend[e.Backend]; !ok {
				byBackend[e.Backend] = nil
				order = append(order, e.Backend)
			}
		}
	}

	out := backendsResponse{Backends: make([]backendGroup, 0, len(order))}
	for _, name := range order {
		bal := balances[name]
		if n, nerr := haproxy.NormalizeBalance(bal); nerr == nil {
			bal = n
		} else {
			bal = haproxy.DefaultBalance
		}
		srv := byBackend[name]
		if srv == nil {
			srv = []haproxy.ServerInfo{}
		}
		out.Backends = append(out.Backends, backendGroup{
			Name:    name,
			Balance: bal,
			Servers: srv,
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
	Balance string `json:"balance"`
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
	balance, err := haproxy.NormalizeBalance(req.Balance)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// HAProxy rejects spaces/punctuation in server names (`server DE Selectel OUT …`
	// is parsed as unknown keyword OUT and the backend ends up with zero servers).
	req.Name = haproxy.SanitizeName(req.Name)

	srv := store.Server{
		Backend: req.Backend,
		Name:    req.Name,
		Address: req.Address,
		Port:    req.Port,
		Weight:  req.Weight,
	}
	if err := d.Store.UpsertWithBalance(srv, balance); err != nil {
		writeErr(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}

	if err := d.writeHAProxyConfig(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		"ok":      true,
		"server":  srv,
		"balance": balance,
	})
}

func (d Deps) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	backend := chi.URLParam(r, "backend")
	rawName := chi.URLParam(r, "name")
	if backend == "" || rawName == "" {
		writeErr(w, http.StatusBadRequest, "backend and name are required")
		return
	}
	name := haproxy.SanitizeName(rawName)

	removed, err := d.Store.Delete(backend, rawName)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed && name != rawName {
		removed, err = d.Store.Delete(backend, name)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if !removed {
		writeErr(w, http.StatusNotFound, "server not found in store")
		return
	}

	if err := d.writeHAProxyConfig(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Prefer disable then delete via runtime; ignore failures and reload.
	_ = d.HA.SetServerState(backend, name, "maint")
	_ = d.HA.SetServerState(backend, rawName, "maint")
	if err := d.HA.DelServerRuntime(backend, name); err != nil {
		_ = d.HA.DelServerRuntime(backend, rawName)
		d.Log.Info("runtime del skipped/failed, will reload", "err", err)
	}
	reloadWarn := ""
	if err := d.Docker.Reload(r.Context()); err != nil {
		d.Log.Error("reload after delete", "err", err)
		reloadWarn = "сервер удалён, но reload HAProxy не удался: " + err.Error()
	} else if err := d.waitHAReady(r.Context()); err != nil {
		d.Log.Error("haproxy not ready after delete reload", "err", err)
		reloadWarn = "сервер удалён, но HAProxy ещё не готов: " + err.Error()
	}

	out := map[string]any{"ok": true, "deleted": name, "backend": backend}
	if reloadWarn != "" {
		out["warning"] = reloadWarn
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) handleReload(w http.ResponseWriter, r *http.Request) {
	// Ensure config is synced from store before reload.
	if err := d.writeHAProxyConfig(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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

func (d Deps) handleGetListenPorts(w http.ResponseWriter, r *http.Request) {
	ports, err := listenports.Current(d.Store)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ents, _ := listenports.CurrentEntrances(d.Store)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ports": ports, "entrances": ents})
}

func (d Deps) handlePutListenPorts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ports []int `json:"ports"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	newPorts, opened, closed, err := listenports.Apply(r.Context(), d.Store, d.BackendsDir, d.Docker, d.HA, body.Ports)
	if err != nil {
		d.Log.Error("listen ports", "err", err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	ents, _ := listenports.CurrentEntrances(d.Store)
	if err := d.writeHAProxyConfig(); err != nil {
		d.Log.Warn("ensure backends after listen-ports", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"ports":     newPorts,
		"entrances": ents,
		"opened":    opened,
		"closed":    closed,
	})
}

func (d Deps) handleGetEntrances(w http.ResponseWriter, r *http.Request) {
	ents, err := listenports.CurrentEntrances(d.Store)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"entrances": ents,
		"ports":     store.PortsFromEntrances(ents),
	})
}

func (d Deps) handlePutEntrances(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Entrances []store.Entrance `json:"entrances"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ents, opened, closed, err := listenports.ApplyEntrances(r.Context(), d.Store, d.BackendsDir, d.Docker, d.HA, body.Entrances)
	if err != nil {
		d.Log.Error("entrances", "err", err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if err := d.writeHAProxyConfig(); err != nil {
		d.Log.Warn("ensure backends after entrances", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"entrances": ents,
		"ports":     store.PortsFromEntrances(ents),
		"opened":    opened,
		"closed":    closed,
	})
}

func (d Deps) handleGetProtect(w http.ResponseWriter, r *http.Request) {
	p, err := protect.Current(d.Store)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "protect": p})
}

func (d Deps) handlePutProtect(w http.ResponseWriter, r *http.Request) {
	var body protect.Profile
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p, err := protect.Apply(r.Context(), d.Store, d.BackendsDir, d.Docker, d.HA, body)
	if err != nil {
		d.Log.Error("protect", "err", err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "protect": p})
}

// waitHAReady blocks until the admin socket answers after restart/reload.
func (d Deps) waitHAReady(ctx context.Context) error {
	if d.HA == nil {
		return nil
	}
	return d.HA.WaitReady(ctx)
}

func (d Deps) writeHAProxyConfig() error {
	all, err := d.Store.List()
	if err != nil {
		return err
	}
	balances, err := d.Store.Balances()
	if err != nil {
		return err
	}
	ents, err := d.Store.Entrances()
	if err != nil {
		return err
	}
	ensure := make([]string, 0, len(ents))
	for _, e := range ents {
		ensure = append(ensure, e.Backend)
	}
	if err := d.Cfg.Write(all, balances, ensure...); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
