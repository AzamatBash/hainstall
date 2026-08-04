package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/provision"
	"github.com/azabash/hapanel/panel/internal/store"
)

type Server struct {
	store  *store.Store
	auth   *auth.Service
	agent  *agent.Client
	logger *slog.Logger
	mux    *http.ServeMux
}

func New(st *store.Store, au *auth.Service, ag *agent.Client, logger *slog.Logger) *Server {
	s := &Server{
		store:  st,
		auth:   au,
		agent:  ag,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.withCORS(s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/auth/logout", s.requireAuth(s.handleLogout))
	s.mux.HandleFunc("GET /api/auth/me", s.requireAuth(s.handleMe))

	s.mux.HandleFunc("GET /api/nodes", s.requireAuth(s.handleListNodes))
	s.mux.HandleFunc("POST /api/nodes", s.requireAuth(s.handleCreateNode))
	s.mux.HandleFunc("POST /api/nodes/provision", s.requireAuth(s.handleProvisionNode))
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.requireAuth(s.handleDeleteNode))
	s.mux.HandleFunc("GET /api/nodes/{id}/install", s.requireAuth(s.handleNodeInstall))
	s.mux.HandleFunc("POST /api/nodes/{id}/connect", s.requireAuth(s.handleConnectNode))

	s.mux.HandleFunc("GET /api/nodes/{id}/stats", s.requireAuth(s.handleNodeStats))
	s.mux.HandleFunc("GET /api/nodes/{id}/system", s.requireAuth(s.handleNodeSystem))
	s.mux.HandleFunc("GET /api/nodes/{id}/backends", s.requireAuth(s.handleNodeBackends))
	s.mux.HandleFunc("POST /api/nodes/{id}/backends", s.requireAuth(s.handleAddBackend))
	s.mux.HandleFunc("DELETE /api/nodes/{id}/backends/{backend}/{name}", s.requireAuth(s.handleDeleteBackend))
	s.mux.HandleFunc("POST /api/nodes/{id}/haproxy/reload", s.requireAuth(s.handleReload))
	s.mux.HandleFunc("POST /api/nodes/{id}/haproxy/restart", s.requireAuth(s.handleRestart))
	s.mux.HandleFunc("GET /api/nodes/{id}/health", s.requireAuth(s.handleHealth))
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := s.auth.TokenFromRequest(r)
		if tok == "" || s.auth.ParseToken(tok) != nil {
			writeErr(w, http.StatusUnauthorized, "нет доступа")
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if locked, msg := s.auth.Limiter().Check(ip); locked {
		writeErr(w, http.StatusTooManyRequests, msg)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if !s.auth.CheckPassword(body.Password) {
		if locked, msg := s.auth.Limiter().RecordFailure(ip); locked {
			writeErr(w, http.StatusTooManyRequests, msg)
			return
		}
		writeErr(w, http.StatusUnauthorized, "неверный пароль")
		return
	}
	s.auth.Limiter().Clear(ip)
	tok, exp, err := s.auth.IssueToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось выдать токен")
		return
	}
	s.auth.SetSessionCookie(w, tok, exp)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"token":   tok,
		"expires": exp.Format(time.RFC3339),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"user": "admin",
	})
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		s.logger.Error("list nodes", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, publicNode(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.URL = strings.TrimRight(strings.TrimSpace(body.URL), "/")
	body.Token = strings.TrimSpace(body.Token)
	if body.Name == "" || body.URL == "" || body.Token == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя, URL и токен")
		return
	}
	if !strings.HasPrefix(body.URL, "https://") && !strings.HasPrefix(body.URL, "http://") {
		writeErr(w, http.StatusBadRequest, "URL должен начинаться с https:// или http://")
		return
	}
	n, err := s.store.CreateNode(body.Name, body.URL, body.Token)
	if err != nil {
		s.logger.Error("create node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, publicNode(*n))
}

func (s *Server) handleProvisionNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Host = strings.TrimSpace(body.Host)
	if body.Port == 0 {
		body.Port = provision.DefaultMgmtPort
	}
	if body.Name == "" || body.Host == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя и хост")
		return
	}

	bundle, err := provision.Generate(body.Name, body.Host, body.Port, "")
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	n, err := s.store.CreateNode(body.Name, bundle.URL, bundle.Token)
	if err != nil {
		s.logger.Error("provision node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	_ = s.store.UpdateNodeStatus(n.ID, store.StatusUnknown, nil)

	writeJSON(w, http.StatusCreated, map[string]any{
		"node":   publicNode(*n),
		"bundle": bundle,
	})
}

func (s *Server) handleNodeInstall(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	bundle, err := provision.GenerateFromURL(n.Name, n.URL, n.Token)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node":   publicNode(*n),
		"bundle": bundle,
	})
}

func (s *Server) handleConnectNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}

	// Reachability: public health. Auth: /system. HAProxy readiness: /stats
	// with retries (socket appears a few seconds after container start).
	now := time.Now().UTC()
	hStatus, hBody, err := s.agent.Health(r.Context(), n.URL, n.Token)
	if err != nil {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"error":  err.Error(),
			"node":   publicNodeMust(s, id),
		})
		return
	}
	if hStatus < 200 || hStatus >= 300 {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"status": hStatus,
			"body":   string(hBody),
			"error":  fmt.Sprintf("агент не отвечает на /health: %d", hStatus),
			"node":   publicNodeMust(s, id),
		})
		return
	}

	sysStatus, sysBody, err := s.agent.System(r.Context(), n.URL, n.Token)
	if err != nil {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"error":  err.Error(),
			"node":   publicNodeMust(s, id),
		})
		return
	}
	if sysStatus == http.StatusUnauthorized {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"status": sysStatus,
			"error":  "ошибка авторизации — неверный токен на ноде",
			"node":   publicNodeMust(s, id),
		})
		return
	}
	if sysStatus < 200 || sysStatus >= 300 {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"status": sysStatus,
			"body":   string(sysBody),
			"error":  fmt.Sprintf("неожиданный ответ агента /system: %d", sysStatus),
			"node":   publicNodeMust(s, id),
		})
		return
	}

	var statsStatus int
	var statsBody []byte
	var statsErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-r.Context().Done():
				_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
				writeJSON(w, http.StatusOK, map[string]any{
					"ok":     false,
					"online": false,
					"error":  r.Context().Err().Error(),
					"node":   publicNodeMust(s, id),
				})
				return
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		statsStatus, statsBody, statsErr = s.agent.Stats(r.Context(), n.URL, n.Token)
		if statsErr == nil && statsStatus >= 200 && statsStatus < 300 {
			break
		}
		// 502 = HAProxy socket not ready yet; keep retrying.
		if statsErr != nil || statsStatus != http.StatusBadGateway {
			break
		}
	}

	if statsErr != nil || statsStatus < 200 || statsStatus >= 300 {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		errMsg := "HAProxy stats недоступны"
		if statsErr != nil {
			errMsg = statsErr.Error()
		} else if statsStatus == http.StatusBadGateway {
			errMsg = "агент отвечает, но нет связи с HAProxy (admin.sock) — проверьте compose/сокет"
		} else {
			errMsg = fmt.Sprintf("неожиданный ответ агента /stats: %d", statsStatus)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"online": false,
			"status": statsStatus,
			"body":   string(statsBody),
			"error":  errMsg,
			"node":   publicNodeMust(s, id),
		})
		return
	}

	_ = s.store.UpdateNodeStatus(id, store.StatusOnline, &now)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"online": true,
		"status": statsStatus,
		"body":   string(statsBody),
		"node":   publicNodeMust(s, id),
	})
}

func publicNodeMust(s *Server, id string) map[string]any {
	n, err := s.store.GetNode(id)
	if err != nil || n == nil {
		return map[string]any{"id": id}
	}
	return publicNode(*n)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.store.DeleteNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleNodeStats(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		status, body, err := s.agent.Stats(ctx, n.URL, n.Token)
		if err != nil || status < 200 || status >= 300 {
			return status, body, err
		}
		return status, filterStatsJSON(body), nil
	})
}

func (s *Server) handleNodeSystem(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		return s.agent.System(ctx, n.URL, n.Token)
	})
}

func (s *Server) handleNodeBackends(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		status, body, err := s.agent.Backends(ctx, n.URL, n.Token)
		if err != nil || status < 200 || status >= 300 {
			return status, body, err
		}
		return status, filterBackendsJSON(body), nil
	})
}

func (s *Server) handleAddBackend(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
		return
	}
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		return s.agent.AddBackend(ctx, n.URL, n.Token, strings.NewReader(string(body)))
	})
}

func (s *Server) handleDeleteBackend(w http.ResponseWriter, r *http.Request) {
	backend := r.PathValue("backend")
	name := r.PathValue("name")
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		return s.agent.DeleteBackend(ctx, n.URL, n.Token, backend, name)
	})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		status, body, _, err := s.agent.ReloadResilient(ctx, n.URL, n.Token)
		return status, body, err
	})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		status, body, _, err := s.agent.RestartResilient(ctx, n.URL, n.Token)
		return status, body, err
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.proxyNode(w, r, func(ctx context.Context, n *store.Node) (int, []byte, error) {
		return s.agent.Health(ctx, n.URL, n.Token)
	})
}

type nodeProxyFn func(ctx context.Context, n *store.Node) (int, []byte, error)

func (s *Server) proxyNode(w http.ResponseWriter, r *http.Request, fn nodeProxyFn) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}

	status, body, err := fn(r.Context(), n)
	now := time.Now().UTC()
	if err != nil {
		s.logger.Warn("agent request failed", "node", id, "err", err)
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
		writeErr(w, http.StatusBadGateway, "агент недоступен: "+err.Error())
		return
	}

	if status >= 200 && status < 500 {
		st := store.StatusOnline
		if status >= 400 {
			st = store.StatusOnline // auth/app errors still mean reachable
		}
		_ = s.store.UpdateNodeStatus(id, st, &now)
	} else {
		_ = s.store.UpdateNodeStatus(id, store.StatusOffline, &now)
	}

	ct := "application/json"
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func publicNode(n store.Node) map[string]any {
	m := map[string]any{
		"id":         n.ID,
		"name":       n.Name,
		"url":        n.URL,
		"created_at": n.CreatedAt.Format(time.RFC3339),
		"status":     n.Status,
	}
	if n.LastSeen != nil {
		m["last_seen"] = n.LastSeen.Format(time.RFC3339)
	}
	return m
}

// isInternalBackend hides management backends (hap_agent, acme, …) from panel UI/API.
func isInternalBackend(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "hap_agent", "acme", "stats", "prometheus":
		return true
	}
	if strings.HasPrefix(n, "hap_") {
		return true
	}
	if strings.HasSuffix(n, "_mgmt") || strings.HasSuffix(n, "_management") {
		return true
	}
	return false
}

func filterBackendsJSON(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	raw, ok := payload["backends"]
	if !ok {
		return body
	}
	groups, ok := raw.([]any)
	if !ok {
		return body
	}
	filtered := make([]any, 0, len(groups))
	for _, g := range groups {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			name, _ = m["backend"].(string)
		}
		if isInternalBackend(name) {
			continue
		}
		filtered = append(filtered, m)
	}
	payload["backends"] = filtered
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func filterStatsJSON(body []byte) []byte {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return body
	}
	raw, ok := payload["backends"]
	if !ok {
		return body
	}
	groups, ok := raw.([]any)
	if !ok {
		return body
	}
	filtered := make([]any, 0, len(groups))
	sessions := 0
	for _, g := range groups {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if isInternalBackend(name) {
			continue
		}
		filtered = append(filtered, m)
		switch v := m["sessions"].(type) {
		case float64:
			sessions += int(v)
		case int:
			sessions += v
		}
	}
	payload["backends"] = filtered
	payload["active_sessions"] = sessions
	out, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
