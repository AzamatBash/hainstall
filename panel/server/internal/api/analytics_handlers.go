package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

func (s *Server) handleAnalyticsNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListRemnaNodeCatalog()
	if err != nil {
		s.logger.Error("analytics list nodes", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) handleAnalyticsNodePatch(w http.ResponseWriter, r *http.Request) {
	panelID := strings.TrimSpace(r.PathValue("panelId"))
	uuid := strings.TrimSpace(r.PathValue("uuid"))
	if panelID == "" || uuid == "" {
		writeErr(w, http.StatusBadRequest, "нужны panelId и uuid")
		return
	}
	var body struct {
		ProtocolOverride   *string `json:"protocol_override"`
		RoleRNFront        *bool   `json:"role_rn_front"`
		RoleRNBack         *bool   `json:"role_rn_back"`
		RoleHPBack         *bool   `json:"role_hp_back"`
		EnabledInAnalytics *bool   `json:"enabled_in_analytics"`
		Notes              *string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	n, err := s.store.PatchRemnaNodeCatalog(panelID, uuid, store.RemnaNodePatch{
		ProtocolOverride:   body.ProtocolOverride,
		RoleRNFront:        body.RoleRNFront,
		RoleRNBack:         body.RoleRNBack,
		RoleHPBack:         body.RoleHPBack,
		EnabledInAnalytics: body.EnabledInAnalytics,
		Notes:              body.Notes,
	})
	if err != nil {
		s.logger.Error("analytics patch node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена — сначала sync из Remnawave")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": n})
}

func (s *Server) handleAnalyticsNodesSync(w http.ResponseWriter, r *http.Request) {
	if s.remnaStats == nil {
		writeErr(w, http.StatusServiceUnavailable, "poller не запущен")
		return
	}
	ctx := r.Context()
	s.remnaStats.SyncNow(ctx)
	nodes, err := s.store.ListRemnaNodeCatalog()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "nodes": nodes, "count": len(nodes)})
}

func (s *Server) handleAnalyticsWeek(w http.ResponseWriter, r *http.Request) {
	hours := 168
	if v := strings.TrimSpace(r.URL.Query().Get("hours")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hours = n
		}
	}
	if hours > 14*24 {
		hours = 14 * 24
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	out, err := s.store.BuildWeekAnalytics(since)
	if err != nil {
		s.logger.Error("analytics week", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
