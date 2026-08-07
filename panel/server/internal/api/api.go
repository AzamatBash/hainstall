package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/agent"
	"github.com/azabash/hapanel/panel/internal/auth"
	"github.com/azabash/hapanel/panel/internal/opsagent"
	"github.com/azabash/hapanel/panel/internal/provision"
	"github.com/azabash/hapanel/panel/internal/remna"
	"github.com/azabash/hapanel/panel/internal/store"
)

type Options struct {
	SecretsKey  string
	GeminiKey   string
	GroqKey     string
	LLMProvider string
	PanelIP     string
	LLMProxy    string
}

type Server struct {
	store      *store.Store
	auth       *auth.Service
	agent      *agent.Client
	remna      *remna.Client
	ops        *opsagent.Runner
	secretsKey string
	logger     *slog.Logger
	mux        *http.ServeMux

	remnaCacheMu         sync.Mutex
	remnaCache           map[string]remnaStatsCacheEntry
	remnaLiveCache       map[string]remnaLiveCacheEntry
	remnaPanelNodesCache map[string]remnaPanelNodesCacheEntry
	remnaOnlineByNode    map[string]remnaOnlineCacheEntry
	remnaRefreshInflight map[string]bool
}

func New(st *store.Store, au *auth.Service, ag *agent.Client, logger *slog.Logger, opt Options) *Server {
	llm := &opsagent.LLM{
		Provider:  opt.LLMProvider,
		GeminiKey: opt.GeminiKey,
		GroqKey:   opt.GroqKey,
	}
	if opt.LLMProxy != "" {
		hc, err := opsagent.NewHTTPClient(opt.LLMProxy, 90*time.Second)
		if err != nil {
			logger.Error("llm proxy", "err", err)
		} else {
			llm.HTTPClient = hc
			logger.Info("llm http client via proxy configured")
		}
	}
	s := &Server{
		store:                st,
		auth:                 au,
		agent:                ag,
		remna:                remna.New(),
		ops:                  opsagent.NewRunner(st, llm, opt.PanelIP, logger),
		secretsKey:           opt.SecretsKey,
		logger:               logger,
		mux:                  http.NewServeMux(),
		remnaCache:           make(map[string]remnaStatsCacheEntry),
		remnaLiveCache:       make(map[string]remnaLiveCacheEntry),
		remnaPanelNodesCache: make(map[string]remnaPanelNodesCacheEntry),
		remnaOnlineByNode:    make(map[string]remnaOnlineCacheEntry),
		remnaRefreshInflight: make(map[string]bool),
	}
	s.ops.OnNodeReady = func(nodeID string) {
		n, err := s.store.GetNode(nodeID)
		if err != nil || n == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		now := time.Now().UTC()
		hStatus, _, err := s.agent.Health(ctx, n.URL, n.Token)
		if err != nil || hStatus < 200 || hStatus >= 300 {
			_ = s.store.UpdateNodeStatus(nodeID, store.StatusOffline, &now)
			return
		}
		_ = s.store.UpdateNodeStatus(nodeID, store.StatusOnline, &now)
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
	s.mux.HandleFunc("PUT /api/nodes/reorder", s.requireAuth(s.handleReorderNodes))
	s.mux.HandleFunc("PATCH /api/nodes/{id}", s.requireAuth(s.handleUpdateNode))
	s.mux.HandleFunc("DELETE /api/nodes/{id}", s.requireAuth(s.handleDeleteNode))
	s.mux.HandleFunc("GET /api/nodes/{id}/install", s.requireAuth(s.handleNodeInstall))
	s.mux.HandleFunc("POST /api/nodes/{id}/connect", s.requireAuth(s.handleConnectNode))

	s.mux.HandleFunc("GET /api/nodes/{id}/stats", s.requireAuth(s.handleNodeStats))
	s.mux.HandleFunc("GET /api/nodes/{id}/system", s.requireAuth(s.handleNodeSystem))
	s.mux.HandleFunc("GET /api/nodes/{id}/backends", s.requireAuth(s.handleNodeBackends))
	s.mux.HandleFunc("POST /api/nodes/{id}/backends", s.requireAuth(s.handleAddBackend))
	s.mux.HandleFunc("DELETE /api/nodes/{id}/backends/{backend}/{name}", s.requireAuth(s.handleDeleteBackend))
	s.mux.HandleFunc("PUT /api/nodes/{id}/backends/{backend}/{name}/remna", s.requireAuth(s.handleUpsertBackendRemna))
	s.mux.HandleFunc("GET /api/nodes/{id}/backends/remna-stats", s.requireAuth(s.handleNodeRemnaStats))
	s.mux.HandleFunc("GET /api/nodes/{id}/backends/remna-links", s.requireAuth(s.handleNodeRemnaLinks))
	s.mux.HandleFunc("POST /api/nodes/{id}/haproxy/reload", s.requireAuth(s.handleReload))
	s.mux.HandleFunc("POST /api/nodes/{id}/haproxy/restart", s.requireAuth(s.handleRestart))
	s.mux.HandleFunc("GET /api/nodes/{id}/health", s.requireAuth(s.handleHealth))

	s.mux.HandleFunc("GET /api/remna-panels", s.requireAuth(s.handleListRemnaPanels))
	s.mux.HandleFunc("POST /api/remna-panels", s.requireAuth(s.handleCreateRemnaPanel))
	s.mux.HandleFunc("PUT /api/remna-panels/{id}", s.requireAuth(s.handleUpdateRemnaPanel))
	s.mux.HandleFunc("DELETE /api/remna-panels/{id}", s.requireAuth(s.handleDeleteRemnaPanel))

	s.mux.HandleFunc("GET /api/providers", s.requireAuth(s.handleListProviders))
	s.mux.HandleFunc("POST /api/providers", s.requireAuth(s.handleCreateProvider))
	s.mux.HandleFunc("PUT /api/providers/{id}", s.requireAuth(s.handleUpdateProvider))
	s.mux.HandleFunc("DELETE /api/providers/{id}", s.requireAuth(s.handleDeleteProvider))
	s.mux.HandleFunc("POST /api/providers/{id}/accounts", s.requireAuth(s.handleCreateProviderAccount))
	s.mux.HandleFunc("PUT /api/provider-accounts/{id}", s.requireAuth(s.handleUpdateProviderAccount))
	s.mux.HandleFunc("DELETE /api/provider-accounts/{id}", s.requireAuth(s.handleDeleteProviderAccount))

	s.mux.HandleFunc("POST /api/agent/deploy", s.requireAuth(s.handleAgentDeploy))
	s.mux.HandleFunc("GET /api/agent/jobs/{id}", s.requireAuth(s.handleAgentJob))
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
	remnaAddrs := map[string][]string{}
	if links, err := s.store.ListAllBackendRemnaLinks(); err == nil {
		for _, l := range links {
			addr := strings.TrimSpace(l.RemnaAddress)
			if addr == "" {
				continue
			}
			remnaAddrs[l.NodeID] = append(remnaAddrs[l.NodeID], addr)
		}
	}
	// Never wait on Remnawave for the list: serve cached Online and warm in background.
	s.scheduleRemnaOnlineRefresh(nodes)
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := s.publicNode(n)
		addrs := remnaAddrs[n.ID]
		if addrs == nil {
			addrs = []string{}
		}
		m["remna_addresses"] = addrs
		m["remna_online"] = s.nodeRemnaOnlineCached(n.ID)
		out = append(out, m)
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
	writeJSON(w, http.StatusCreated, s.publicNode(*n))
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
		"node":   s.publicNode(*n),
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
		"node":   s.publicNode(*n),
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
			errMsg = "агент отвечает, но нет связи с HAProxy runtime API — проверьте HAPROXY_SOCKET (tcp://haproxy:9999) и stats socket в haproxy.cfg"
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
	return s.publicNode(*n)
}

func (s *Server) handleReorderNodes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if len(body.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, "укажите ids")
		return
	}
	existing, err := s.store.ListNodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if len(body.IDs) != len(existing) {
		writeErr(w, http.StatusBadRequest, "ids должен содержать все ноды")
		return
	}
	seen := make(map[string]struct{}, len(body.IDs))
	for _, id := range body.IDs {
		if id == "" {
			writeErr(w, http.StatusBadRequest, "пустой id")
			return
		}
		if _, ok := seen[id]; ok {
			writeErr(w, http.StatusBadRequest, "дублирующийся id")
			return
		}
		seen[id] = struct{}{}
	}
	for _, n := range existing {
		if _, ok := seen[n.ID]; !ok {
			writeErr(w, http.StatusBadRequest, "ids не совпадает со списком нод")
			return
		}
	}
	if err := s.store.ReorderNodes(body.IDs); err != nil {
		s.logger.Error("reorder nodes", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, s.publicNode(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": out})
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Name              string          `json:"name"`
		Country           *string         `json:"country"`
		RemnaPanelID      json.RawMessage `json:"remna_panel_id"`
		ProviderID        json.RawMessage `json:"provider_id"`
		ProviderAccountID json.RawMessage `json:"provider_account_id"`
		Host              string          `json:"host"`
		Port              int             `json:"port"`
		URL               string          `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}

	name := strings.TrimSpace(body.Name)
	newURL := strings.TrimRight(strings.TrimSpace(body.URL), "/")
	host := strings.TrimSpace(body.Host)
	hasURLChange := newURL != "" || host != "" || body.Port != 0
	hasName := name != ""
	hasCountry := body.Country != nil
	remnaPanelPtr, hasRemnaPanel, remnaOK := parseOptionalRemnaPanelID(body.RemnaPanelID)
	if !remnaOK {
		writeErr(w, http.StatusBadRequest, "некорректный remna_panel_id")
		return
	}
	providerPtr, hasProvider, providerOK := parseOptionalRemnaPanelID(body.ProviderID)
	if !providerOK {
		writeErr(w, http.StatusBadRequest, "некорректный provider_id")
		return
	}
	accountPtr, hasAccount, accountOK := parseOptionalRemnaPanelID(body.ProviderAccountID)
	if !accountOK {
		writeErr(w, http.StatusBadRequest, "некорректный provider_account_id")
		return
	}
	if hasRemnaPanel && remnaPanelPtr != nil && *remnaPanelPtr != "" {
		p, err := s.store.GetRemnaPanel(*remnaPanelPtr)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if p == nil {
			writeErr(w, http.StatusBadRequest, "remna-панель не найдена")
			return
		}
	}
	if hasProvider && providerPtr != nil && *providerPtr != "" {
		p, err := s.store.GetProvider(*providerPtr)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if p == nil {
			writeErr(w, http.StatusBadRequest, "провайдер не найден")
			return
		}
	}
	if hasAccount && accountPtr != nil && *accountPtr != "" {
		a, err := s.store.GetProviderAccount(*accountPtr)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if a == nil {
			writeErr(w, http.StatusBadRequest, "аккаунт провайдера не найден")
			return
		}
		effectiveProvider := n.ProviderID
		if hasProvider && providerPtr != nil {
			effectiveProvider = *providerPtr
		}
		if a.ProviderID != effectiveProvider {
			writeErr(w, http.StatusBadRequest, "аккаунт не принадлежит выбранному провайдеру")
			return
		}
	}

	if hasCountry {
		c := strings.ToUpper(strings.TrimSpace(*body.Country))
		if c != "" && !isCountryCode(c) {
			writeErr(w, http.StatusBadRequest, "некорректный код страны")
			return
		}
		body.Country = &c
	}

	// Meta-only update: name and/or country and/or remna panel / provider / account, keep URL and status.
	if !hasURLChange {
		if !hasName && !hasCountry && !hasRemnaPanel && !hasProvider && !hasAccount {
			writeErr(w, http.StatusBadRequest, "укажите name, country, remna_panel_id, provider_id, provider_account_id, host или url")
			return
		}
		var namePtr *string
		if hasName {
			namePtr = &name
		}
		updated, err := s.store.UpdateNodeMeta(id, namePtr, body.Country, remnaPanelPtr, providerPtr, accountPtr)
		if err != nil {
			s.logger.Error("update node", "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if updated == nil {
			writeErr(w, http.StatusNotFound, "нода не найдена")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": s.publicNode(*updated)})
		return
	}

	if newURL == "" {
		port := body.Port
		if host == "" && port == 0 {
			writeErr(w, http.StatusBadRequest, "укажите host или url")
			return
		}
		curHost, curPort, err := provision.ParseURL(n.URL)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "текущий URL ноды повреждён: "+err.Error())
			return
		}
		if host == "" {
			host = curHost
		}
		if port == 0 {
			port = curPort
		}
		newURL = provision.BuildURL(host, port)
	}
	if !strings.HasPrefix(newURL, "https://") && !strings.HasPrefix(newURL, "http://") {
		writeErr(w, http.StatusBadRequest, "URL должен начинаться с https:// или http://")
		return
	}
	if _, _, err := provision.ParseURL(newURL); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	updated, err := s.store.UpdateNodeURL(id, name, newURL)
	if err != nil {
		s.logger.Error("update node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if updated == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if hasCountry || hasRemnaPanel || hasProvider || hasAccount {
		var countryPtr *string
		if hasCountry {
			countryPtr = body.Country
		}
		var remnaPtr *string
		if hasRemnaPanel {
			remnaPtr = remnaPanelPtr
		}
		var provPtr *string
		if hasProvider {
			provPtr = providerPtr
		}
		var accPtr *string
		if hasAccount {
			accPtr = accountPtr
		}
		updated, err = s.store.UpdateNodeMeta(id, nil, countryPtr, remnaPtr, provPtr, accPtr)
		if err != nil {
			s.logger.Error("update node meta", "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": s.publicNode(*updated)})
}

// parseOptionalRemnaPanelID: absent → no change; null or "" → clear; otherwise set.
func parseOptionalRemnaPanelID(raw json.RawMessage) (value *string, set bool, ok bool) {
	if len(raw) == 0 {
		return nil, false, true
	}
	if string(raw) == "null" {
		empty := ""
		return &empty, true, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, false, false
	}
	s = strings.TrimSpace(s)
	return &s, true, true
}

func isCountryCode(code string) bool {
	if len(code) != 2 {
		return false
	}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
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

func (s *Server) publicNode(n store.Node) map[string]any {
	m := map[string]any{
		"id":                   n.ID,
		"name":                 n.Name,
		"url":                  n.URL,
		"country":              n.Country,
		"sort_order":           n.SortOrder,
		"remna_panel_id":       n.RemnaPanelID,
		"remna_panel_name":     "",
		"provider_id":          n.ProviderID,
		"provider_name":        "",
		"provider_favicon":     "",
		"provider_login_url":   "",
		"provider_account_id":  n.ProviderAccountID,
		"provider_account_login": "",
		"created_at":           n.CreatedAt.Format(time.RFC3339),
		"status":               n.Status,
	}
	if n.RemnaPanelID != "" {
		if p, err := s.store.GetRemnaPanel(n.RemnaPanelID); err == nil && p != nil {
			m["remna_panel_name"] = p.Name
		}
	}
	if n.ProviderID != "" {
		if p, err := s.store.GetProvider(n.ProviderID); err == nil && p != nil {
			m["provider_name"] = p.Name
			m["provider_favicon"] = p.FaviconURL
			m["provider_login_url"] = p.LoginURL
		}
	}
	if n.ProviderAccountID != "" {
		if a, err := s.store.GetProviderAccount(n.ProviderAccountID); err == nil && a != nil {
			m["provider_account_login"] = a.Login
		}
	}
	if n.LastSeen != nil {
		m["last_seen"] = n.LastSeen.Format(time.RFC3339)
	}
	if n.Snapshot != nil {
		m["live"] = n.Snapshot
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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
