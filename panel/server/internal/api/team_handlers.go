package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleTeamBoard(w http.ResponseWriter, r *http.Request) {
	members, err := s.store.ListTeamMembers()
	if err != nil {
		s.logger.Error("list team", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	marks, err := s.store.ListTeamDays()
	if err != nil {
		s.logger.Error("list team days", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	outMembers := make([]map[string]any, 0, len(members))
	for _, m := range members {
		outMembers = append(outMembers, map[string]any{
			"id":         m.ID,
			"name":       m.Name,
			"color":      m.Color,
			"created_at": m.CreatedAt.Format(time.RFC3339),
		})
	}
	outMarks := make([]map[string]any, 0, len(marks))
	for _, m := range marks {
		outMarks = append(outMarks, map[string]any{
			"member_id":  m.MemberID,
			"day":        m.Day,
			"status":     m.Status,
			"note":       m.Note,
			"updated_at": m.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"members": outMembers,
		"marks":   outMarks,
	})
}

func (s *Server) handleCreateTeamMember(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	m, err := s.store.CreateTeamMember(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"member": map[string]any{
			"id":         m.ID,
			"name":       m.Name,
			"color":      m.Color,
			"created_at": m.CreatedAt.Format(time.RFC3339),
		},
	})
}

func (s *Server) handleRenameTeamMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	m, err := s.store.RenameTeamMember(id, body.Name)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "человек не найден")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"member": map[string]any{
			"id":    m.ID,
			"name":  m.Name,
			"color": m.Color,
		},
	})
}

func (s *Server) handleDeleteTeamMember(w http.ResponseWriter, r *http.Request) {
	ok, err := s.store.DeleteTeamMember(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "человек не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetTeamDay(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MemberID string `json:"member_id"`
		Day      string `json:"day"`
		Status   string `json:"status"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.MemberID = strings.TrimSpace(body.MemberID)
	body.Day = strings.TrimSpace(body.Day)
	body.Status = strings.TrimSpace(body.Status)
	if !isCalendarDay(body.Day) {
		writeErr(w, http.StatusBadRequest, "day — дата YYYY-MM-DD")
		return
	}
	if err := s.store.SetTeamDay(body.MemberID, body.Day, body.Status, body.Note); err != nil {
		if strings.Contains(err.Error(), "не найден") {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func isCalendarDay(raw string) bool {
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return false
	}
	return t.Format("2006-01-02") == raw
}
