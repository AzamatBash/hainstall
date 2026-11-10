package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/azabash/hapanel/panel/internal/proxyconfig"
	"github.com/azabash/hapanel/panel/internal/store"
)

// GET /api/proxy-profiles
func (s *Server) handleListProxyProfiles(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProxyProfiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": list})
}

// GET /api/proxy-profiles/defaults?engine=haproxy|envoy
func (s *Server) handleProxyProfileDefaults(w http.ResponseWriter, r *http.Request) {
	engine := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("engine")))
	if engine == "" {
		engine = store.ProxyEngineHAProxy
	}
	if engine != store.ProxyEngineHAProxy && engine != store.ProxyEngineEnvoy {
		writeErr(w, http.StatusBadRequest, "engine: haproxy или envoy")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"engine": engine,
		"body":   proxyconfig.DefaultBody(engine),
		"note":   "Стандартный VPN-шаблон hapanel (как на нодах).",
	})
}

// POST /api/proxy-profiles/validate
func (s *Server) handleValidateProxyProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Engine string `json:"engine"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	res := proxyconfig.Validate(body.Engine, body.Body)
	writeJSON(w, http.StatusOK, res)
}

// POST /api/proxy-profiles
func (s *Server) handleCreateProxyProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		Engine string `json:"engine"`
		Body   string `json:"body"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	engine := strings.TrimSpace(body.Engine)
	if engine == "" {
		engine = store.ProxyEngineHAProxy
	}
	cfg := body.Body
	if strings.TrimSpace(cfg) == "" {
		cfg = proxyconfig.DefaultBody(engine)
	}
	vres := proxyconfig.Validate(engine, cfg)
	if !vres.OK {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":      vres.Message,
			"validation": vres,
		})
		return
	}
	p, err := s.store.CreateProxyProfile(body.Name, engine, cfg, body.Note)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"profile": p, "validation": vres})
}

// GET /api/proxy-profiles/{id}
func (s *Server) handleGetProxyProfile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	p, err := s.store.GetProxyProfile(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusNotFound, "профиль не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p})
}

// PUT /api/proxy-profiles/{id}
func (s *Server) handleUpdateProxyProfile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	cur, err := s.store.GetProxyProfile(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if cur == nil {
		writeErr(w, http.StatusNotFound, "профиль не найден")
		return
	}
	var body struct {
		Name   *string `json:"name"`
		Engine *string `json:"engine"`
		Body   *string `json:"body"`
		Note   *string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	name := cur.Name
	engine := cur.Engine
	cfg := cur.Body
	note := cur.Note
	bump := false
	if body.Name != nil {
		name = *body.Name
	}
	if body.Engine != nil {
		engine = *body.Engine
	}
	if body.Body != nil {
		cfg = *body.Body
		if cfg != cur.Body {
			bump = true
		}
	}
	if body.Note != nil {
		note = *body.Note
	}
	vres := proxyconfig.Validate(engine, cfg)
	if !vres.OK {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":      vres.Message,
			"validation": vres,
		})
		return
	}
	p, err := s.store.UpdateProxyProfile(id, name, engine, cfg, note, bump)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": p, "validation": vres})
}

// DELETE /api/proxy-profiles/{id}
func (s *Server) handleDeleteProxyProfile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	ok, err := s.store.DeleteProxyProfile(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "профиль не найден")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// PUT /api/nodes/{id}/proxy-profile — panel-only assignment; does not touch the agent.
func (s *Server) handleAssignNodeProxyProfile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		ProfileID *string `json:"proxy_profile_id"`
		Engine    string  `json:"proxy_engine"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if body.ProfileID == nil {
		writeErr(w, http.StatusBadRequest, "укажите proxy_profile_id (пусто = снять)")
		return
	}
	n, err := s.store.AssignNodeProxyProfile(id, *body.ProfileID, body.Engine)
	if err != nil {
		if strings.Contains(err.Error(), "не найден") || strings.Contains(err.Error(), "не совпадает") || strings.Contains(err.Error(), "engine") {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"node": n,
		"note": "Назначение сохранено в панели. На ноду пока не отправляется — нужен обновлённый агент.",
	})
}

// PUT /api/nodes/{id}/proxy-engine — preferred engine in panel only.
func (s *Server) handleSetNodeProxyEngine(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		Engine string `json:"proxy_engine"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	n, err := s.store.SetNodeProxyEngine(id, body.Engine)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node": n})
}

// POST /api/nodes/{id}/proxy-apply — push assigned profile to the node agent.
func (s *Server) handleNodeProxyApply(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	n, err := s.store.GetNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if strings.TrimSpace(n.ProxyProfileID) == "" {
		writeErr(w, http.StatusBadRequest, "сначала назначьте шаблон на ноде")
		return
	}
	p, err := s.store.GetProxyProfile(n.ProxyProfileID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if p == nil {
		writeErr(w, http.StatusBadRequest, "профиль не найден")
		return
	}
	if p.Engine != store.ProxyEngineHAProxy {
		writeErr(w, http.StatusBadRequest, "агент пока применяет только HAProxy")
		return
	}

	payload, _ := json.Marshal(map[string]any{
		"profile_id": p.ID,
		"revision":   p.Revision,
		"engine":     p.Engine,
		"body":       p.Body,
	})
	ctx := r.Context()
	status, raw, err := s.agent.ProxyApply(ctx, n.URL, n.Token, strings.NewReader(string(payload)))
	if err != nil {
		_, _ = s.store.SetNodeProxyApplied(id, n.ProxyAppliedRev, err.Error())
		writeErr(w, http.StatusBadGateway, "агент: "+err.Error())
		return
	}
	if status < 200 || status >= 300 {
		msg := strings.TrimSpace(string(raw))
		var eresp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &eresp) == nil && eresp.Error != "" {
			msg = eresp.Error
		}
		if msg == "" {
			msg = "агент отклонил apply"
		}
		_, _ = s.store.SetNodeProxyApplied(id, n.ProxyAppliedRev, msg)
		writeErr(w, http.StatusBadGateway, msg)
		return
	}

	n2, err := s.store.SetNodeProxyApplied(id, p.Revision, "")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "профиль на ноде применён, но статус в панели не сохранился")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"node":  n2,
		"agent": json.RawMessage(raw),
		"note":  "Шаблон записан в base.cfg; backend-серверы на ноде сохранены из state агента.",
	})
}
