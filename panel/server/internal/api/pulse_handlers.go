package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/remna"
)

// GET /api/remna-panels/{id}/pulse — local SQLite spans only (Remnawave synced in background).
func (s *Server) handleRemnaPanelPulse(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "укажите id панели")
		return
	}
	panel, err := s.store.GetRemnaPanel(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if panel == nil {
		writeErr(w, http.StatusNotFound, "панель не найдена")
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

	meta, err := s.store.GetPulseSync(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	spans, err := s.store.ListPulseSpans(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}

	if refresh && s.pulseSync != nil {
		s.pulseSync.SyncPanelForce(id)
	}

	if len(spans) == 0 {
		if s.pulseSync != nil {
			s.pulseSync.SyncPanelForce(id)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"panel_id":   id,
			"panel_name": panel.Name,
			"syncing":    true,
			"error":      "Первая синхронизация Pulse ещё идёт — обновите через пару минут. Remnawave не дёргаем при каждом открытии.",
		})
		return
	}

	remnaSpans := make([]remna.UserSpan, 0, len(spans))
	for _, sp := range spans {
		remnaSpans = append(remnaSpans, remna.UserSpan{
			Created:  sp.Created,
			Expire:   sp.Expire,
			Revoked:  sp.Revoked,
			Disabled: sp.Disabled,
		})
	}

	st := remna.UserStatusCounts{}
	syncedAt := time.Time{}
	if meta != nil {
		st = remna.UserStatusCounts{
			Active:   meta.ActiveNow,
			Disabled: meta.DisabledNow,
			Limited:  meta.LimitedNow,
			Expired:  meta.ExpiredNow,
			Total:    meta.TotalUsers,
		}
		syncedAt = meta.SyncedAt
	}

	asOf := time.Now()
	if !syncedAt.IsZero() {
		asOf = syncedAt
	}
	pulse := remna.BuildPulseFromSpans(remnaSpans, st, tz, months, asOf)

	syncErr := ""
	if meta != nil {
		syncErr = meta.Err
	}

	syncing := false
	if s.pulseSync != nil {
		syncing = s.pulseSync.InFlight(id)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"panel_id":        id,
		"panel_name":      panel.Name,
		"active_now":      pulse.ActiveNow,
		"active_by_dates": pulse.ActiveByDates,
		"disabled_now":    pulse.DisabledNow,
		"limited_now":     pulse.LimitedNow,
		"expired_now":     pulse.ExpiredNow,
		"total_users":     pulse.TotalUsers,
		"users_sampled":   pulse.UsersSampled,
		"tz":              pulse.TZ,
		"as_of":           pulse.AsOf.Format(time.RFC3339),
		"synced_at":       formatPulseSyncedAt(syncedAt),
		"sync_error":      syncErr,
		"syncing":         syncing,
		"week_days":       pulse.WeekDays,
		"week_total":      pulse.WeekTotal,
		"months":          pulse.Months,
		"note":            pulse.Note,
		"scale": map[string]any{
			"monthly": map[string]any{
				"source": "VPN avg churn ~5.5%/мес (RetentionCheck); solid B2C <5% (PureVPN)",
			},
		},
	})
}

func formatPulseSyncedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
