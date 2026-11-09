package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GET /api/remna-panels/{id}/pulse — inflow/outflow/net tables (week + months).
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

	ctx := r.Context()
	// Digest batch can take a few seconds against Remnawave.
	pulse, err := s.remna.FetchPulse(ctx, cred.baseURL, cred.apiKey, tz, months)
	if err != nil {
		s.logger.Error("remna pulse", "panel", id, "err", err)
		writeErr(w, http.StatusBadGateway, "не удалось получить digest Remnawave: "+err.Error())
		return
	}

	panel, _ := s.store.GetRemnaPanel(id)
	name := id
	if panel != nil {
		name = panel.Name
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"panel_id":     id,
		"panel_name":   name,
		"active_now":   pulse.ActiveNow,
		"disabled_now": pulse.DisabledNow,
		"limited_now":  pulse.LimitedNow,
		"expired_now":  pulse.ExpiredNow,
		"total_users":  pulse.TotalUsers,
		"tz":           pulse.TZ,
		"as_of":        pulse.AsOf.Format(time.RFC3339),
		"week_days":    pulse.WeekDays,
		"week_total":   pulse.WeekTotal,
		"months":       pulse.Months,
		"note":         pulse.Note,
		"scale": map[string]any{
			"monthly": map[string]any{
				"outflow": []any{
					map[string]any{"max": 0.03, "level": "excellent", "label": "Отлично"},
					map[string]any{"max": 0.07, "level": "ok", "label": "Нормально"},
					map[string]any{"max": 0.10, "level": "weak", "label": "Слабо"},
					map[string]any{"max": nil, "level": "bad", "label": "Плохо"},
				},
				"inflow": []any{
					map[string]any{"min": 0.08, "level": "excellent", "label": "Отлично"},
					map[string]any{"min": 0.05, "level": "ok", "label": "Нормально"},
					map[string]any{"min": 0.03, "level": "weak", "label": "Слабо"},
					map[string]any{"min": nil, "level": "bad", "label": "Плохо"},
				},
				"net": []any{
					map[string]any{"min": 0.03, "level": "excellent", "label": "Отлично"},
					map[string]any{"min": 0.0, "level": "ok", "label": "Нормально"},
					map[string]any{"min": -0.03, "level": "weak", "label": "Слабо"},
					map[string]any{"min": nil, "level": "bad", "label": "Плохо"},
				},
				"source": "VPN avg churn ~5.5%/мес (RetentionCheck); solid B2C <5% (PureVPN)",
			},
		},
	})
}
