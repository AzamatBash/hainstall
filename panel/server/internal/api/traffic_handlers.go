package api

import (
	"net/http"
	"time"
)

func (s *Server) handleNodeTraffic(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	since := time.Now().UTC().Add(-time.Hour)
	points, err := s.store.ListTrafficSamples(id, since)
	if err != nil {
		s.logger.Error("list traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node_id": id,
		"window":  "1h",
		"points":  points,
	})
}

func (s *Server) handleAllTraffic(w http.ResponseWriter, r *http.Request) {
	since := time.Now().UTC().Add(-time.Hour)
	byNode, err := s.store.ListAllTrafficSamples(since)
	if err != nil {
		s.logger.Error("list all traffic", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window": "1h",
		"nodes":  byNode,
	})
}
