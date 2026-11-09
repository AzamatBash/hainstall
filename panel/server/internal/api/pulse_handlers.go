package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/remna"
)

const pulseSpansTTL = 10 * time.Minute

// GET /api/remna-panels/{id}/pulse — inflow/outflow/net from createdAt/expireAt.
func (s *Server) handleRemnaPanelPulse(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "укажите id панели")
		return
	}
	cred := s.loadRemnaPanelCreds(id)
	if cred.err != "" {
		status := http.StatusBadRequest
		if strings.Contains(cred.err, "не найдена") {
			status = http.StatusNotFound
		}
		writeErr(w, status, cred.err)
		return
	}

	months := 5
	if v := strings.TrimSpace(r.URL.Query().Get("months")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 12 {
			writeErr(w, http.StatusBadRequest, "months: 1–12")
			return
		}
		months = n
	}
	tz := strings.TrimSpace(r.URL.Query().Get("tz"))
	if tz == "" {
		tz = "Europe/Moscow"
	}
	refresh := r.URL.Query().Get("refresh") == "1"

	ctx := r.Context()
	spans, err := s.cachedPulseSpans(ctx, id, cred.baseURL, cred.apiKey, refresh)
	if err != nil {
		s.logger.Error("remna pulse users", "panel", id, "err", err)
		writeErr(w, http.StatusBadGateway, "не удалось загрузить пользователей Remnawave: "+err.Error())
		return
	}

	pulse, err := s.remna.FetchPulse(ctx, cred.baseURL, cred.apiKey, tz, months, spans)
	if err != nil {
		s.logger.Error("remna pulse", "panel", id, "err", err)
		writeErr(w, http.StatusBadGateway, "не удалось построить Pulse: "+err.Error())
		return
	}

	panel, _ := s.store.GetRemnaPanel(id)
	name := id
	if panel != nil {
		name = panel.Name
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"panel_id":         id,
		"panel_name":       name,
		"active_now":       pulse.ActiveNow,
		"active_by_dates":  pulse.ActiveByDates,
		"disabled_now":     pulse.DisabledNow,
		"limited_now":      pulse.LimitedNow,
		"expired_now":      pulse.ExpiredNow,
		"total_users":      pulse.TotalUsers,
		"users_sampled":    pulse.UsersSampled,
		"tz":               pulse.TZ,
		"as_of":            pulse.AsOf.Format(time.RFC3339),
		"week_days":        pulse.WeekDays,
		"week_total":       pulse.WeekTotal,
		"months":           pulse.Months,
		"note":             pulse.Note,
		"scale": map[string]any{
			"monthly": map[string]any{
				"source": "VPN avg churn ~5.5%/мес (RetentionCheck); solid B2C <5% (PureVPN)",
			},
		},
	})
}

func (s *Server) cachedPulseSpans(ctx context.Context, panelID, baseURL, apiKey string, refresh bool) ([]remna.UserSpan, error) {
	if !refresh {
		s.pulseSpansMu.Lock()
		ent, ok := s.pulseSpans[panelID]
		s.pulseSpansMu.Unlock()
		if ok && time.Since(ent.at) < pulseSpansTTL && len(ent.spans) > 0 {
			return ent.spans, nil
		}
	}

	spans, err := s.remna.FetchUserSpans(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	s.pulseSpansMu.Lock()
	s.pulseSpans[panelID] = pulseSpansCache{at: time.Now(), spans: spans}
	s.pulseSpansMu.Unlock()
	return spans, nil
}
