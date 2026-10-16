package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

func publicAPIKeyMeta(m store.APIKeyMeta) map[string]any {
	out := map[string]any{
		"id":         m.ID,
		"name":       m.Name,
		"prefix":     m.Prefix,
		"created_at": m.CreatedAt.UTC().Format(time.RFC3339),
		"revoked":    m.RevokedAt != nil,
	}
	if m.LastUsedAt != nil {
		out["last_used_at"] = m.LastUsedAt.UTC().Format(time.RFC3339)
	}
	if m.RevokedAt != nil {
		out["revoked_at"] = m.RevokedAt.UTC().Format(time.RFC3339)
	}
	return out
}

func (s *Server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListAPIKeys()
	if err != nil {
		s.logger.Error("list api keys", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		out = append(out, publicAPIKeyMeta(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": out})
}

func (s *Server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil && err != io.EOF {
			writeErr(w, http.StatusBadRequest, "некорректный JSON")
			return
		}
	}
	created, err := s.store.CreateAPIKey(body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	meta := publicAPIKeyMeta(created.APIKeyMeta)
	meta["key"] = created.Key // plaintext once
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     meta,
		"warning": "Сохраните ключ сейчас — повторно посмотреть его нельзя.",
	})
}

func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	ok, err := s.store.RevokeAPIKey(r.PathValue("id"))
	if err != nil {
		s.logger.Error("revoke api key", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "ключ не найден или уже отозван")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	ok, err := s.store.DeleteAPIKey(r.PathValue("id"))
	if err != nil {
		s.logger.Error("delete api key", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "ключ не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
