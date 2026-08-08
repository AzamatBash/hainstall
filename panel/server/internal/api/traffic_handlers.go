package api

import (
	"net/http"
	"strconv"
	"time"
)

const (
	trafficDefaultHours = 1
	trafficMaxHours     = 24
)

func trafficWindowHours(r *http.Request) int {
	raw := r.URL.Query().Get("hours")
	if raw == "" {
		return trafficDefaultHours
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return trafficDefaultHours
	}
	if n > trafficMaxHours {
		return trafficMaxHours
	}
	return n
}

func (s *Server) handleNodeTraffic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	hours := trafficWindowHours(r)
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	points, err := s.store.ListTrafficSamples(id, since)
	if err != nil {
		s.logger.Error("list traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": id,
		"hours":   hours,
		"window":  strconv.Itoa(hours) + "h",
		"points":  points,
	})
}

func (s *Server) handleAllTraffic(w http.ResponseWriter, r *http.Request) {
	hours := trafficWindowHours(r)
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
