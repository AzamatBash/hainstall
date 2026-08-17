package api

import (
	"net/http"
	"strconv"
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
		samples, err := s.store.ListTrafficHourlySamples(id, since)
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
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id":        id,
			"hours":          hours,
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
