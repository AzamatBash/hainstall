package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	trafficDefaultHours   = 1
	trafficMaxHours       = 24
	trafficHourlyMaxHours = 24 * 90
	trafficHourlySeconds  = 3600.0
)

func trafficWindowHours(r *http.Request, max int) int {
	raw := r.URL.Query().Get("hours")
	if raw == "" {
		return trafficDefaultHours
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return trafficDefaultHours
	}
	if n > max {
		return max
	}
	return n
}

func (s *Server) handleNodeTraffic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hourly := r.URL.Query().Get("granularity") == "hour"
	max := trafficMaxHours
	if hourly {
		max = trafficHourlyMaxHours
	}
	hours := trafficWindowHours(r, max)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	n, err := s.store.GetNode(id)
	if err != nil {
		s.logger.Error("get node traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}

	if hourly {
		from, to, ok := trafficRange(r)
		if !ok {
			hours := trafficWindowHours(r, trafficHourlyMaxHours)
			from = time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
			to = time.Time{}
		}
		if !from.IsZero() && !to.IsZero() && to.Sub(from) > time.Duration(trafficHourlyMaxHours)*time.Hour {
			from = to.Add(-time.Duration(trafficHourlyMaxHours) * time.Hour)
		}
		samples, err := s.store.ListTrafficHourlyRange(id, from, to)
		if err != nil {
			s.logger.Error("list hourly traffic", "err", err)
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		points := make([]map[string]any, 0, len(samples))
		var totalRx, totalTx int64
		for _, p := range samples {
			totalRx += p.RxBytes
			totalTx += p.TxBytes
			points = append(points, map[string]any{
				"t":        p.TS,
				"down_bps": float64(p.TxBytes) / trafficHourlySeconds,
				"up_bps":   float64(p.RxBytes) / trafficHourlySeconds,
				"rx_bytes": p.RxBytes,
				"tx_bytes": p.TxBytes,
			})
		}
		hours := int(time.Since(from).Hours() + 0.5)
		if hours < 1 {
			hours = 1
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":        id,
			"hours":          hours,
			"from":           from.UTC().UnixMilli(),
			"to":             toOrNow(to).UnixMilli(),
			"window":         strconv.Itoa(hours) + "h",
			"granularity":    "hour",
			"traffic_log":    n.TrafficLog,
			"points":         points,
			"total_rx_bytes": totalRx,
			"total_tx_bytes": totalTx,
		})
		return
	}

	points, err := s.store.ListTrafficSamples(id, since)
	if err != nil {
		s.logger.Error("list traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id":     id,
		"hours":       hours,
		"window":      strconv.Itoa(hours) + "h",
		"granularity": "5s",
		"traffic_log": n.TrafficLog,
		"points":      points,
	})
}

func (s *Server) handleAllTraffic(w http.ResponseWriter, r *http.Request) {
	hours := trafficWindowHours(r, trafficMaxHours)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	byNode, err := s.store.ListAllTrafficSamples(since)
	if err != nil {
		s.logger.Error("list all traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hours":  hours,
		"window": strconv.Itoa(hours) + "h",
		"nodes":  byNode,
	})
}

func trafficRange(r *http.Request) (from, to time.Time, ok bool) {
	fromRaw := strings.TrimSpace(r.URL.Query().Get("from"))
	toRaw := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromRaw == "" && toRaw == "" {
		return time.Time{}, time.Time{}, false
	}
	from = parseTrafficTime(fromRaw)
	to = parseTrafficTime(toRaw)
	if from.IsZero() && to.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if to.Before(from) {
		from, to = to, from
	}
	return from, to, true
}

func parseTrafficTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil && ms > 1_000_000_000_000 {
		return time.UnixMilli(ms).UTC()
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func toOrNow(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}
