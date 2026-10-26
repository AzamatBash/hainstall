package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/azabash/hapanel/panel/internal/hoststat"
)

type homeAlertDTO struct {
	PanelID     string `json:"panel_id"`
	PanelName   string `json:"panel_name"`
	RemnaUUID   string `json:"remna_uuid"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	Drop        bool   `json:"drop"`
	Decline     bool   `json:"decline"`
	DropHint    string `json:"drop_hint"`
	DeclineHint string `json:"decline_hint"`
	Note        string `json:"note"`
}

type homeResponse struct {
	hoststat.Snapshot
	Alerts  []homeAlertDTO  `json:"alerts"`
	Traffic *homeTrafficDTO `json:"traffic,omitempty"`
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.loadHomeAlerts()
	if err != nil {
		s.logger.Error("home alerts", "err", err)
		alerts = []homeAlertDTO{}
	}
	if alerts == nil {
		alerts = []homeAlertDTO{}
	}
	traffic, err := s.loadHomeTraffic()
	if err != nil {
		s.logger.Error("home traffic", "err", err)
		traffic = nil
	}
	writeJSON(w, http.StatusOK, homeResponse{
		Snapshot: hoststat.Read(),
		Alerts:   alerts,
		Traffic:  traffic,
	})
}

func (s *Server) handleHomeAlertNote(w http.ResponseWriter, r *http.Request) {
	panelID := strings.TrimSpace(r.PathValue("panelId"))
	uuid := strings.TrimSpace(r.PathValue("uuid"))
	if panelID == "" || uuid == "" {
		writeErr(w, http.StatusBadRequest, "нужны panelId и uuid")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "неверное тело")
		return
	}
	note := strings.TrimSpace(body.Note)
	if utf8.RuneCountInString(note) > 500 {
		writeErr(w, http.StatusBadRequest, "заметка длиннее 500 символов")
		return
	}
	if err := s.store.SetRemnaNodeAlertNote(panelID, uuid, note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "нода не найдена")
			return
		}
		s.logger.Error("save alert note", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"note": note})
}

func (s *Server) loadHomeAlerts() ([]homeAlertDTO, error) {
	s.homeAlertMu.Lock()
	fresh := s.homeAlertRows != nil && time.Since(s.homeAlertAt) < 45*time.Second
	rows := append([]homeAlertDTO(nil), s.homeAlertRows...)
	s.homeAlertMu.Unlock()

	if !fresh {
		built, err := s.computeHomeAlerts()
		if err != nil {
			if rows == nil {
				return nil, err
			}
			s.logger.Error("home alerts refresh", "err", err)
		} else {
			rows = built
			s.homeAlertMu.Lock()
			s.homeAlertAt = time.Now()
			s.homeAlertRows = append([]homeAlertDTO(nil), built...)
			s.homeAlertMu.Unlock()
		}
	}

	notes, err := s.store.MapRemnaNodeAlertNotes()
	if err != nil {
		s.logger.Error("home alert notes", "err", err)
		return rows, nil
	}
	for i := range rows {
		rows[i].Note = notes[rows[i].PanelID+"\x00"+rows[i].RemnaUUID]
	}
	return rows, nil
}

func (s *Server) computeHomeAlerts() ([]homeAlertDTO, error) {
	nodes, err := s.store.ListRemnaNodeCatalog()
	if err != nil {
		return nil, err
	}
	alerts, err := s.loadNodeTrafficAlerts(time.Now().UTC())
	if err != nil {
		return nil, err
	}
	out := make([]homeAlertDTO, 0)
	for _, n := range nodes {
		a := alerts[n.PanelID+"\x00"+n.RemnaUUID]
		if !a.drop && !a.decline {
			continue
		}
		out = append(out, homeAlertDTO{
			PanelID:     n.PanelID,
			PanelName:   n.PanelName,
			RemnaUUID:   n.RemnaUUID,
			Name:        n.Name,
			Address:     n.Address,
			Drop:        a.drop,
			Decline:     a.decline,
			DropHint:    a.dropHint,
			DeclineHint: a.declineHint,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Drop != out[j].Drop {
			return out[i].Drop
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Address < out[j].Address
	})
	return out, nil
}
