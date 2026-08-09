package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/remna"
	"github.com/azabash/hapanel/panel/internal/secretbox"
	"github.com/azabash/hapanel/panel/internal/store"
)

type remnaStatsCacheEntry struct {
	at    time.Time
	stats []map[string]any
}

type remnaLiveCacheEntry struct {
	at    time.Time
	stats remna.LiveStats
}

type remnaPanelNodesCacheEntry struct {
	at    time.Time
	nodes []remna.Node
}

type remnaOnlineCacheEntry struct {
	at    time.Time
	total int
}

type remnaPanelCreds struct {
	baseURL string
	apiKey  string
	err     string
}

const remnaCacheTTL = 12 * time.Second

func (s *Server) handleListRemnaPanels(w http.ResponseWriter, r *http.Request) {
	panels, err := s.store.ListRemnaPanels()
	if err != nil {
		s.logger.Error("list remna panels", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(panels))
	for _, p := range panels {
		out = append(out, s.publicRemnaPanelWithOnline(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"panels": out})
}

func (s *Server) handleCreateRemnaPanel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	body.APIKey = strings.TrimSpace(body.APIKey)
	if body.Name == "" || body.BaseURL == "" || body.APIKey == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя, base_url и api_key")
		return
	}
	if !strings.HasPrefix(body.BaseURL, "https://") && !strings.HasPrefix(body.BaseURL, "http://") {
		writeErr(w, http.StatusBadRequest, "base_url должен начинаться с https:// или http://")
		return
	}
	enc, err := secretbox.Seal(s.secretsKey, []byte(body.APIKey))
	if err != nil {
		s.logger.Error("seal remna api key", "err", err)
		writeErr(w, http.StatusInternalServerError, "не удалось зашифровать ключ")
		return
	}
	p, err := s.store.CreateRemnaPanel(body.Name, body.BaseURL, enc)
	if err != nil {
		s.logger.Error("create remna panel", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, publicRemnaPanel(*p))
}

func (s *Server) handleUpdateRemnaPanel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name    string  `json:"name"`
		BaseURL string  `json:"base_url"`
		APIKey  *string `json:"api_key"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	name := strings.TrimSpace(body.Name)
	baseURL := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	if name == "" && baseURL == "" && body.APIKey == nil {
		writeErr(w, http.StatusBadRequest, "укажите name, base_url или api_key")
		return
	}
	if baseURL != "" && !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "http://") {
		writeErr(w, http.StatusBadRequest, "base_url должен начинаться с https:// или http://")
		return
	}

	existing, err := s.store.GetRemnaPanel(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if existing == nil {
		writeErr(w, http.StatusNotFound, "панель не найдена")
		return
	}
	if name == "" {
		name = existing.Name
	}
	if baseURL == "" {
		baseURL = existing.BaseURL
	}

	var encPtr *[]byte
	if body.APIKey != nil {
		key := strings.TrimSpace(*body.APIKey)
		if key != "" {
			enc, err := secretbox.Seal(s.secretsKey, []byte(key))
			if err != nil {
				s.logger.Error("seal remna api key", "err", err)
				writeErr(w, http.StatusInternalServerError, "не удалось зашифровать ключ")
				return
			}
			encPtr = &enc
		}
		// empty api_key string → keep existing key (encPtr stays nil)
	}

	p, err := s.store.UpdateRemnaPanel(id, name, baseURL, encPtr)
	if err != nil {
		s.logger.Error("update remna panel", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "панель не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "panel": publicRemnaPanel(*p)})
}

func (s *Server) handleDeleteRemnaPanel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.store.DeleteRemnaPanel(id)
	if err != nil {
		s.logger.Error("delete remna panel", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "панель не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleUpsertBackendRemna(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	backend := r.PathValue("backend")
	name := r.PathValue("name")
	if nodeID == "" || backend == "" || name == "" {
		writeErr(w, http.StatusBadRequest, "некорректный путь")
		return
	}
	n, err := s.store.GetNode(nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "не удалось прочитать тело запроса")
		return
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		ok, err := s.store.DeleteBackendRemnaLink(nodeID, backend, name)
		if err != nil {
			s.logger.Error("delete remna link", "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		s.invalidateRemnaStatsCache(nodeID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cleared": ok})
		return
	}

	var body struct {
		RemnaPanelID *string `json:"remna_panel_id"`
		RemnaAddress *string `json:"remna_address"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	// Explicit clear: empty/null panel id or remna address.
	clear := body.RemnaPanelID == nil || strings.TrimSpace(*body.RemnaPanelID) == "" ||
		body.RemnaAddress == nil || strings.TrimSpace(*body.RemnaAddress) == ""
	if clear {
		ok, err := s.store.DeleteBackendRemnaLink(nodeID, backend, name)
		if err != nil {
			s.logger.Error("delete remna link", "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		s.invalidateRemnaStatsCache(nodeID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cleared": ok})
		return
	}

	panelID := strings.TrimSpace(*body.RemnaPanelID)
	remnaAddress := strings.TrimSpace(*body.RemnaAddress)
	panel, err := s.store.GetRemnaPanel(panelID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if panel == nil {
		writeErr(w, http.StatusBadRequest, "remna-панель не найдена")
		return
	}

	link, err := s.store.UpsertBackendRemnaLink(nodeID, backend, name, panelID, remnaAddress)
	if err != nil {
		s.logger.Error("upsert remna link", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	s.invalidateRemnaStatsCache(nodeID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "link": publicRemnaLink(*link)})
}

func (s *Server) handleNodeRemnaLinks(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := s.store.GetNode(nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	links, err := s.store.GetBackendRemnaLinks(nodeID)
	if err != nil {
		s.logger.Error("list remna links", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(links))
	for _, l := range links {
		out = append(out, publicRemnaLink(l))
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

func (s *Server) handleNodeRemnaStats(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := s.store.GetNode(nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}

	if cached, ok := s.getRemnaStatsCache(nodeID); ok {
		writeJSON(w, http.StatusOK, map[string]any{"stats": cached})
		return
	}

	links, err := s.store.GetBackendRemnaLinks(nodeID)
	if err != nil {
		s.logger.Error("list remna links", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}

	credsCache := map[string]remnaPanelCreds{}
	stats := make([]map[string]any, 0, len(links))

	for _, l := range links {
		row := map[string]any{
			"backend":        l.Backend,
			"name":           l.ServerName,
			"remna_panel_id": l.RemnaPanelID,
			"remna_address":  l.RemnaAddress,
			"online":         false,
			"users_online":   nil,
			"down_bps":       nil,
			"up_bps":         nil,
			"ram_percent":    nil,
			"load_avg":       nil,
			"cpu_count":      nil,
			"traffic_used":   nil,
			"traffic_limit":  nil,
			"error":          "",
			"missing":        false,
		}

		cred, ok := credsCache[l.RemnaPanelID]
		if !ok {
			cred = s.loadRemnaPanelCreds(l.RemnaPanelID)
			credsCache[l.RemnaPanelID] = cred
		}
		if cred.err != "" {
			row["error"] = cred.err
			stats = append(stats, row)
			continue
		}

		live := s.cachedRemnaLiveStats(r.Context(), l.RemnaPanelID, l.RemnaAddress, cred)
		if live.Error != "" && !live.Found {
			row["error"] = live.Error
			stats = append(stats, row)
			continue
		}
		if !live.Found {
			row["missing"] = true
			if live.Error != "" {
				row["error"] = live.Error
			}
			stats = append(stats, row)
			continue
		}
		row["online"] = live.Online
		if live.UsersOnline != nil {
			row["users_online"] = *live.UsersOnline
		}
		if live.DownBps != nil {
			row["down_bps"] = *live.DownBps
		}
		if live.UpBps != nil {
			row["up_bps"] = *live.UpBps
		}
		if live.RAMPercent != nil {
			row["ram_percent"] = *live.RAMPercent
		}
		if len(live.LoadAvg) > 0 {
			row["load_avg"] = live.LoadAvg
		}
		if live.CPUCount != nil {
			row["cpu_count"] = *live.CPUCount
		}
		if live.TrafficUsed != nil {
			row["traffic_used"] = *live.TrafficUsed
		}
		if live.TrafficLimit != nil {
			row["traffic_limit"] = *live.TrafficLimit
		}
		if live.Error != "" {
			row["error"] = live.Error
		}
		stats = append(stats, row)
	}

	s.putRemnaStatsCache(nodeID, stats)
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats})
}

func (s *Server) loadRemnaPanelCreds(panelID string) remnaPanelCreds {
	p, err := s.store.GetRemnaPanel(panelID)
	if err != nil {
		return remnaPanelCreds{err: "ошибка базы данных"}
	}
	if p == nil {
		return remnaPanelCreds{err: "remna-панель не найдена"}
	}
	if len(p.ApiKeyEnc) == 0 {
		return remnaPanelCreds{err: "api key не задан"}
	}
	plain, err := secretbox.Open(s.secretsKey, p.ApiKeyEnc)
	if err != nil {
		return remnaPanelCreds{err: "не удалось расшифровать api key"}
	}
	return remnaPanelCreds{baseURL: p.BaseURL, apiKey: string(plain)}
}

// nodeRemnaOnlineCached returns last known Online without calling Remnawave.
func (s *Server) nodeRemnaOnlineCached(nodeID string) int {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	if e, ok := s.remnaOnlineByNode[nodeID]; ok {
		return e.total
	}
	return 0
}

// scheduleRemnaOnlineRefresh warms Remnawave caches in the background (at most one run at a time).
func (s *Server) scheduleRemnaOnlineRefresh(nodes []store.Node) {
	s.remnaCacheMu.Lock()
	if s.remnaRefreshInflight["online"] {
		s.remnaCacheMu.Unlock()
		return
	}
	s.remnaRefreshInflight["online"] = true
	s.remnaCacheMu.Unlock()

	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	go func() {
		defer func() {
			s.remnaCacheMu.Lock()
			delete(s.remnaRefreshInflight, "online")
			s.remnaCacheMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, id := range ids {
			_ = s.nodeRemnaOnline(ctx, id)
		}
	}()
}

// nodeRemnaOnline sums Remnawave users_online across backend links for a hapanel node.
func (s *Server) nodeRemnaOnline(ctx context.Context, nodeID string) int {
	links, err := s.store.GetBackendRemnaLinks(nodeID)
	if err != nil || len(links) == 0 {
		s.putRemnaOnlineCache(nodeID, 0)
		return 0
	}
	total := 0
	credsCache := map[string]remnaPanelCreds{}
	panelNodes := map[string][]remna.Node{}
	for _, l := range links {
		cred, ok := credsCache[l.RemnaPanelID]
		if !ok {
			cred = s.loadRemnaPanelCreds(l.RemnaPanelID)
			credsCache[l.RemnaPanelID] = cred
		}
		if cred.err != "" {
			continue
		}
		nodes, ok := panelNodes[l.RemnaPanelID]
		if !ok {
			nodes = s.cachedRemnaPanelNodes(ctx, l.RemnaPanelID, cred)
			panelNodes[l.RemnaPanelID] = nodes
		}
		n := remna.FindNodeByAddress(nodes, l.RemnaAddress)
		if n != nil && n.UsersOnline != nil {
			total += *n.UsersOnline
		}
	}
	s.putRemnaOnlineCache(nodeID, total)
	return total
}

func (s *Server) putRemnaOnlineCache(nodeID string, total int) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	if s.remnaOnlineByNode == nil {
		s.remnaOnlineByNode = make(map[string]remnaOnlineCacheEntry)
	}
	s.remnaOnlineByNode[nodeID] = remnaOnlineCacheEntry{at: time.Now(), total: total}
}

func remnaLiveCacheKey(panelID, address string) string {
	return panelID + "|" + strings.TrimSpace(address)
}

func (s *Server) cachedRemnaLiveStats(ctx context.Context, panelID, address string, cred remnaPanelCreds) remna.LiveStats {
	key := remnaLiveCacheKey(panelID, address)
	if cached, ok := s.getRemnaLiveCache(key); ok {
		return cached
	}
	nodes := s.cachedRemnaPanelNodes(ctx, panelID, cred)
	addr := strings.TrimSpace(address)
	if addr == "" {
		live := remna.LiveStats{Error: "empty remna address"}
		s.putRemnaLiveCache(key, live)
		return live
	}
	n := remna.FindNodeByAddress(nodes, addr)
	if n == nil {
		live := remna.LiveStats{Found: false}
		s.putRemnaLiveCache(key, live)
		return live
	}
	live := remna.LiveFromNode(n)
	// Prefer system.interface; fill speeds from legacy realtime only if missing.
	if live.DownBps == nil && live.UpBps == nil {
		usage, err := s.remna.ListRealtimeUsage(ctx, cred.baseURL, cred.apiKey)
		if err == nil && len(usage) > 0 {
			wantUUID := strings.TrimSpace(n.UUID)
			wantName := strings.ToLower(strings.TrimSpace(n.Name))
			for _, u := range usage {
				match := wantUUID != "" && strings.TrimSpace(u.NodeUUID) == wantUUID
				if !match && wantName != "" && strings.ToLower(strings.TrimSpace(u.NodeName)) == wantName {
					match = true
				}
				if !match {
					continue
				}
				down := u.DownloadSpeedBps
				up := u.UploadSpeedBps
				live.DownBps = &down
				live.UpBps = &up
				break
			}
		}
	}
	s.putRemnaLiveCache(key, live)
	return live
}

func (s *Server) cachedRemnaPanelNodes(ctx context.Context, panelID string, cred remnaPanelCreds) []remna.Node {
	s.remnaCacheMu.Lock()
	if e, ok := s.remnaPanelNodesCache[panelID]; ok && time.Since(e.at) <= remnaCacheTTL {
		nodes := e.nodes
		s.remnaCacheMu.Unlock()
		return nodes
	}
	s.remnaCacheMu.Unlock()

	nodes, err := s.remna.ListNodes(ctx, cred.baseURL, cred.apiKey)
	if err != nil {
		s.logger.Debug("remna list nodes", "panel", panelID, "err", err)
		return nil
	}
	s.remnaCacheMu.Lock()
	if s.remnaPanelNodesCache == nil {
		s.remnaPanelNodesCache = make(map[string]remnaPanelNodesCacheEntry)
	}
	s.remnaPanelNodesCache[panelID] = remnaPanelNodesCacheEntry{at: time.Now(), nodes: nodes}
	s.remnaCacheMu.Unlock()
	return nodes
}

func (s *Server) getRemnaLiveCache(key string) (remna.LiveStats, bool) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	e, ok := s.remnaLiveCache[key]
	if !ok || time.Since(e.at) > remnaCacheTTL {
		return remna.LiveStats{}, false
	}
	return e.stats, true
}

func (s *Server) putRemnaLiveCache(key string, stats remna.LiveStats) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	if s.remnaLiveCache == nil {
		s.remnaLiveCache = make(map[string]remnaLiveCacheEntry)
	}
	s.remnaLiveCache[key] = remnaLiveCacheEntry{at: time.Now(), stats: stats}
}

func (s *Server) getRemnaStatsCache(nodeID string) ([]map[string]any, bool) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	e, ok := s.remnaCache[nodeID]
	if !ok || time.Since(e.at) > remnaCacheTTL {
		return nil, false
	}
	return e.stats, true
}

func (s *Server) putRemnaStatsCache(nodeID string, stats []map[string]any) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	if s.remnaCache == nil {
		s.remnaCache = make(map[string]remnaStatsCacheEntry)
	}
	s.remnaCache[nodeID] = remnaStatsCacheEntry{at: time.Now(), stats: stats}
}

func (s *Server) invalidateRemnaStatsCache(nodeID string) {
	s.remnaCacheMu.Lock()
	defer s.remnaCacheMu.Unlock()
	delete(s.remnaCache, nodeID)
	delete(s.remnaOnlineByNode, nodeID)
	// Link changes should refresh Online promptly.
	clear(s.remnaLiveCache)
	clear(s.remnaPanelNodesCache)
}

func publicRemnaPanel(p store.RemnaPanel) map[string]any {
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"base_url":    p.BaseURL,
		"has_api_key": p.HasAPIKey,
		"created_at":  p.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) publicRemnaPanelWithOnline(p store.RemnaPanel) map[string]any {
	m := publicRemnaPanel(p)
	if sample, err := s.store.LatestRemnaOnlineSample(p.ID); err == nil && sample != nil {
		m["online"] = sample.Online
		m["online_at"] = time.UnixMilli(sample.TS).UTC().Format(time.RFC3339)
	}
	if sample, err := s.store.LatestRemnaTrafficSample(p.ID); err == nil && sample != nil {
		m["traffic"] = sample.Bytes
		m["traffic_at"] = time.UnixMilli(sample.TS).UTC().Format(time.RFC3339)
	}
	if s.remnaStats != nil {
		if last, ok := s.remnaStats.LastFor(p.ID); ok {
			if last.Err != "" {
				m["online_error"] = last.Err
				m["traffic_error"] = last.Err
				if !last.At.IsZero() && m["online_at"] == nil {
					m["online_at"] = last.At.Format(time.RFC3339)
				}
				if !last.At.IsZero() && m["traffic_at"] == nil {
					m["traffic_at"] = last.At.Format(time.RFC3339)
				}
			} else {
				m["online"] = last.Online
				m["traffic"] = last.Traffic
				if !last.At.IsZero() {
					m["online_at"] = last.At.Format(time.RFC3339)
					m["traffic_at"] = last.At.Format(time.RFC3339)
				}
				delete(m, "online_error")
				delete(m, "traffic_error")
			}
		}
	}
	return m
}

const (
	remnaOnlineDefaultHours = 24
	remnaOnlineMaxHours     = 31 * 24
)

func remnaOnlineWindowHours(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("hours"))
	if raw == "" {
		return remnaOnlineDefaultHours
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return remnaOnlineDefaultHours
		}
		n = n*10 + int(c-'0')
		if n > remnaOnlineMaxHours {
			return remnaOnlineMaxHours
		}
	}
	if n <= 0 {
		return remnaOnlineDefaultHours
	}
	if n > remnaOnlineMaxHours {
		return remnaOnlineMaxHours
	}
	return n
}

func (s *Server) handleRemnaPanelOnline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.GetRemnaPanel(id)
	if err != nil {
		s.logger.Error("get remna panel", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "remna-панель не найдена")
		return
	}
	hours := remnaOnlineWindowHours(r)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	points, err := s.store.ListRemnaOnlineSamples(id, since)
	if err != nil {
		s.logger.Error("list remna online", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := map[string]any{
		"panel_id": id,
		"hours":    hours,
		"points":   points,
	}
	if s.remnaStats != nil {
		if last, ok := s.remnaStats.LastFor(id); ok {
			if last.Err != "" {
				out["online_error"] = last.Err
				if !last.At.IsZero() {
					out["online_at"] = last.At.Format(time.RFC3339)
				}
			} else {
				out["current"] = last.Online
				if !last.At.IsZero() {
					out["online_at"] = last.At.Format(time.RFC3339)
				}
			}
		}
	}
	if _, has := out["current"]; !has {
		if sample, err := s.store.LatestRemnaOnlineSample(id); err == nil && sample != nil {
			out["current"] = sample.Online
			if out["online_at"] == nil {
				out["online_at"] = time.UnixMilli(sample.TS).UTC().Format(time.RFC3339)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRemnaPanelTraffic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.GetRemnaPanel(id)
	if err != nil {
		s.logger.Error("get remna panel", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "remna-панель не найдена")
		return
	}
	hours := remnaOnlineWindowHours(r)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	points, err := s.store.ListRemnaTrafficSamples(id, since)
	if err != nil {
		s.logger.Error("list remna traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := map[string]any{
		"panel_id": id,
		"hours":    hours,
		"points":   points,
	}
	if s.remnaStats != nil {
		if last, ok := s.remnaStats.LastFor(id); ok {
			if last.Err != "" {
				out["traffic_error"] = last.Err
				if !last.At.IsZero() {
					out["traffic_at"] = last.At.Format(time.RFC3339)
				}
			} else {
				out["current"] = last.Traffic
				if !last.At.IsZero() {
					out["traffic_at"] = last.At.Format(time.RFC3339)
				}
			}
		}
	}
	if _, has := out["current"]; !has {
		if sample, err := s.store.LatestRemnaTrafficSample(id); err == nil && sample != nil {
			out["current"] = sample.Bytes
			if out["traffic_at"] == nil {
				out["traffic_at"] = time.UnixMilli(sample.TS).UTC().Format(time.RFC3339)
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func publicRemnaLink(l store.BackendRemnaLink) map[string]any {
	return map[string]any{
		"node_id":        l.NodeID,
		"backend":        l.Backend,
		"name":           l.ServerName,
		"remna_panel_id": l.RemnaPanelID,
		"remna_address":  l.RemnaAddress,
	}
}
