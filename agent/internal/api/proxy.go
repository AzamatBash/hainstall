package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/azabash/hapanel/agent/internal/haproxy"
)

func (d Deps) handleProxyStatus(w http.ResponseWriter, r *http.Request) {
	st, err := d.Store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{
		"ok":             true,
		"panel_managed":  st.PanelProxy != nil && strings.TrimSpace(st.PanelProxy.Body) != "",
		"servers_count":  len(st.Servers),
		"agent_version":  Version,
	}
	if st.PanelProxy != nil {
		out["profile_id"] = st.PanelProxy.ProfileID
		out["revision"] = st.PanelProxy.Revision
		out["engine"] = st.PanelProxy.Engine
		out["applied_at"] = st.PanelProxy.AppliedAt
	}
	writeJSON(w, http.StatusOK, out)
}

func (d Deps) handleProxyValidate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Engine string `json:"engine"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	base, stripped := haproxy.StripBackendSections(body.Body)
	if err := haproxy.ValidateBaseProfile(base); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "message": err.Error(), "backends_stripped": stripped,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "message": "base profile OK", "backends_stripped": stripped,
	})
}

func (d Deps) handleProxyApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProfileID string `json:"profile_id"`
		Revision  int    `json:"revision"`
		Engine    string `json:"engine"`
		Body      string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	res, err := haproxy.ApplyPanelProfile(r.Context(), d.Store, d.BackendsDir, d.Cfg, d.Docker, d.HA, haproxy.PanelApplyRequest{
		ProfileID: body.ProfileID,
		Revision:  body.Revision,
		Engine:    body.Engine,
		Body:      body.Body,
	})
	if err != nil {
		d.Log.Error("proxy apply", "err", err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}
