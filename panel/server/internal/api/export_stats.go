package api

import (
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

const exportStatsMaxHours = 24 * 90

// requireAPIKeyOrAuth allows either PANEL_API_KEY (Bearer / X-API-Key) or admin JWT.
func (s *Server) requireAPIKeyOrAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKeyOK(r) {
			next(w, r)
			return
		}
		tok := s.auth.TokenFromRequest(r)
		if tok != "" && s.auth.ParseToken(tok) == nil {
			next(w, r)
			return
		}
		writeErr(w, http.StatusUnauthorized, "нет доступа")
	}
}

func (s *Server) apiKeyFromRequest(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func (s *Server) apiKeyOK(r *http.Request) bool {
	got := s.apiKeyFromRequest(r)
	if got == "" {
		return false
	}
	// Env bootstrap key (optional).
	if s.apiKey != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.apiKey)) == 1 {
		return true
	}
	// Keys generated in the panel UI (hashed in SQLite).
	return s.store.ValidAPIKey(got)
}

// handleExportHanodes returns a compact live snapshot for all hanodes
// (CPU, load, traffic rates) for external collectors.
//
// GET /api/v1/hanodes
// Optional: ?hours=24 — include period_rx_bytes / period_tx_bytes from hourly buckets.
func (s *Server) handleExportHanodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		s.logger.Error("export hanodes list", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	hours := exportPeriodHours(r)
	var since time.Time
	if hours > 0 {
		since = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		row := exportHanodeRow(n)
		if hours > 0 {
			samples, err := s.store.ListTrafficHourlySamples(n.ID, since)
			if err != nil {
				s.logger.Error("export hanode hourly", "node", n.ID, "err", err)
			} else {
				var rx, tx int64
				for _, p := range samples {
					rx += p.RxBytes
					tx += p.TxBytes
				}
				row["period_hours"] = hours
				row["period_rx_bytes"] = rx
				row["period_tx_bytes"] = tx
			}
		}
		out = append(out, row)
	}
	resp := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"count":        len(out),
		"nodes":        out,
	}
	if hours > 0 {
		resp["period_hours"] = hours
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleExportHanode returns one hanode snapshot.
//
// GET /api/v1/hanodes/{id}
func (s *Server) handleExportHanode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetNode(id)
	if err != nil {
		s.logger.Error("export hanode get", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	row := exportHanodeRow(*n)
	hours := exportPeriodHours(r)
	if hours > 0 {
		since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
		samples, err := s.store.ListTrafficHourlySamples(n.ID, since)
		if err != nil {
			s.logger.Error("export hanode hourly", "node", n.ID, "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		var rx, tx int64
		for _, p := range samples {
			rx += p.RxBytes
			tx += p.TxBytes
		}
		row["period_hours"] = hours
		row["period_rx_bytes"] = rx
		row["period_tx_bytes"] = tx
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"node":         row,
	})
}

func exportPeriodHours(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("hours"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0
	}
	if n > exportStatsMaxHours {
		return exportStatsMaxHours
	}
	return n
}

func exportHanodeRow(n store.Node) map[string]any {
	m := map[string]any{
		"id":      n.ID,
		"name":    n.Name,
		"country": n.Country,
		"status":  n.Status,
	}
	if n.LastSeen != nil {
		m["last_seen"] = n.LastSeen.UTC().Format(time.RFC3339)
	}
	if snap := n.Snapshot; snap != nil {
		if snap.CPU != nil {
			m["cpu"] = *snap.CPU
		}
		if len(snap.LoadAvg) > 0 {
			m["load_avg"] = snap.LoadAvg
		}
		if snap.DownBps != nil {
			m["down_bps"] = *snap.DownBps
		}
		if snap.UpBps != nil {
			m["up_bps"] = *snap.UpBps
		}
		if snap.Sessions != nil {
			m["sessions"] = *snap.Sessions
		}
		if snap.NetRx > 0 {
			m["net_rx_bytes"] = snap.NetRx
		}
		if snap.NetTx > 0 {
			m["net_tx_bytes"] = snap.NetTx
		}
		if snap.AgentVersion != "" {
			m["agent_version"] = snap.AgentVersion
		}
		if !snap.UpdatedAt.IsZero() {
			m["updated_at"] = snap.UpdatedAt.UTC().Format(time.RFC3339)
		}
	}
	return m
}
