package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/azabash/hapanel/panel/internal/opsagent"
)

func (s *Server) handleAgentDeploy(w http.ResponseWriter, r *http.Request) {
	if s.ops == nil {
		writeErr(w, http.StatusServiceUnavailable, "агент не настроен")
		return
	}
	var body struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		SSHUser     string `json:"ssh_user"`
		SSHPassword string `json:"ssh_password"`
		SSHPort     int    `json:"ssh_port"`
		MgmtPort    int    `json:"mgmt_port"`
		PanelIP     string `json:"panel_ip"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	job, err := s.ops.StartDeploy(opsagent.DeployRequest{
		Name:        body.Name,
		Host:        body.Host,
		SSHUser:     body.SSHUser,
		SSHPassword: body.SSHPassword,
		SSHPort:     body.SSHPort,
		MgmtPort:    body.MgmtPort,
		PanelIP:     strings.TrimSpace(body.PanelIP),
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job": publicJob(job),
	})
}

func (s *Server) handleAgentJob(w http.ResponseWriter, r *http.Request) {
	if s.ops == nil {
		writeErr(w, http.StatusServiceUnavailable, "агент не настроен")
		return
	}
	id := r.PathValue("id")
	job := s.ops.GetJob(id)
	if job == nil {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":      publicJob(job),
		"messages": job.Messages(),
	})
}

func publicJob(j *opsagent.Job) map[string]any {
	m := map[string]any{
		"id":         j.ID,
		"status":     j.Status,
		"node_id":    j.NodeID,
		"created_at": j.Created.Format("2006-01-02T15:04:05Z"),
	}
	if j.Finished != nil {
		m["finished_at"] = j.Finished.Format("2006-01-02T15:04:05Z")
	}
	return m
}
