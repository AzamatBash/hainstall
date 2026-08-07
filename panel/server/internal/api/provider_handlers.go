package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/azabash/hapanel/panel/internal/store"
)

func publicProviderAccount(a store.ProviderAccount) map[string]any {
	return map[string]any{
		"id":          a.ID,
		"provider_id": a.ProviderID,
		"login":       a.Login,
		"created_at":  a.CreatedAt.Format(time.RFC3339),
	}
}

func publicProvider(p store.Provider, accounts []store.ProviderAccount) map[string]any {
	accOut := make([]map[string]any, 0, len(accounts))
	for _, a := range accounts {
		accOut = append(accOut, publicProviderAccount(a))
	}
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"favicon_url": p.FaviconURL,
		"login_url":   p.LoginURL,
		"created_at":  p.CreatedAt.Format(time.RFC3339),
		"accounts":    accOut,
	}
}

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.ListProviders()
	if err != nil {
		s.logger.Error("list providers", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	allAccounts, err := s.store.ListAllProviderAccounts()
	if err != nil {
		s.logger.Error("list provider accounts", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	byProvider := map[string][]store.ProviderAccount{}
	for _, a := range allAccounts {
		byProvider[a.ProviderID] = append(byProvider[a.ProviderID], a)
	}
	out := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		acc := byProvider[p.ID]
		if acc == nil {
			acc = []store.ProviderAccount{}
		}
		out = append(out, publicProvider(p, acc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (s *Server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		FaviconURL string `json:"favicon_url"`
		LoginURL   string `json:"login_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.FaviconURL = strings.TrimSpace(body.FaviconURL)
	body.LoginURL = strings.TrimSpace(body.LoginURL)
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя")
		return
	}
	p, err := s.store.CreateProvider(body.Name, body.FaviconURL, body.LoginURL)
	if err != nil {
		s.logger.Error("create provider", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, publicProvider(*p, nil))
}

func (s *Server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name       string `json:"name"`
		FaviconURL string `json:"favicon_url"`
		LoginURL   string `json:"login_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.FaviconURL = strings.TrimSpace(body.FaviconURL)
	body.LoginURL = strings.TrimSpace(body.LoginURL)
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя")
		return
	}
	p, err := s.store.UpdateProvider(id, body.Name, body.FaviconURL, body.LoginURL)
	if err != nil {
		s.logger.Error("update provider", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "провайдер не найден")
		return
	}
	accounts, err := s.store.ListProviderAccounts(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": publicProvider(*p, accounts)})
}

func (s *Server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.store.DeleteProvider(id)
	if err != nil {
		s.logger.Error("delete provider", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "провайдер не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateProviderAccount(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("id")
	p, err := s.store.GetProvider(providerID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "провайдер не найден")
		return
	}
	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Login = strings.TrimSpace(body.Login)
	if body.Login == "" {
		writeErr(w, http.StatusBadRequest, "укажите логин аккаунта")
		return
	}
	a, err := s.store.CreateProviderAccount(providerID, body.Login)
	if err != nil {
		s.logger.Error("create provider account", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, publicProviderAccount(*a))
}

func (s *Server) handleUpdateProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Login = strings.TrimSpace(body.Login)
	if body.Login == "" {
		writeErr(w, http.StatusBadRequest, "укажите логин аккаунта")
		return
	}
	a, err := s.store.UpdateProviderAccount(id, body.Login)
	if err != nil {
		s.logger.Error("update provider account", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if a == nil {
		writeErr(w, http.StatusNotFound, "аккаунт не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "account": publicProviderAccount(*a)})
}

func (s *Server) handleDeleteProviderAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.store.DeleteProviderAccount(id)
	if err != nil {
		s.logger.Error("delete provider account", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "аккаунт не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
