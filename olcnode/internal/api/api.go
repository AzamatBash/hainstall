package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/azabash/hapanel/olcnode/internal/auth"
	"github.com/azabash/hapanel/olcnode/internal/store"
)

// Version is the agent API version.
const Version = "0.1.0"

// Deps holds handler dependencies.
type Deps struct {
	Log      *slog.Logger
	Auth     auth.Bearer
	Store    *store.Store
	NodeName string
	Started  time.Time
}

// NewRouter builds the HTTP router under /_olcnode/v1 (and legacy /_olcrtc/v1).
func NewRouter(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.Started.IsZero() {
		d.Started = time.Now()
	}

	mux := http.NewServeMux()
	for _, prefix := range []string{"/_olcnode/v1", "/_olcrtc/v1"} {
		mux.HandleFunc("GET "+prefix+"/health", d.handleHealth)

		authed := d.Auth.Middleware
		mux.Handle("GET "+prefix+"/status", authed(http.HandlerFunc(d.handleStatus)))
		mux.Handle("GET "+prefix+"/instances", authed(http.HandlerFunc(d.handleListInstances)))
		mux.Handle("POST "+prefix+"/instances", authed(http.HandlerFunc(d.handleCreateInstance)))
		mux.Handle("PUT "+prefix+"/instances/{id}", authed(http.HandlerFunc(d.handleUpdateInstance)))
		mux.Handle("DELETE "+prefix+"/instances/{id}", authed(http.HandlerFunc(d.handleDeleteInstance)))
		mux.Handle("POST "+prefix+"/instances/{id}/restart", authed(http.HandlerFunc(d.handleRestartInstance)))
		mux.Handle("POST "+prefix+"/deploy", authed(http.HandlerFunc(d.handleDeploy)))
		mux.Handle("GET "+prefix+"/uri/{id}", authed(http.HandlerFunc(d.handleURI)))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(rw, r)
		d.Log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", time.Since(start).String(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (d Deps) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"version":   Version,
		"node_name": d.NodeName,
	})
}

func (d Deps) handleStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := d.Store.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"deployed":   st.Deployed,
		"instances":  len(st.Instances),
		"status":     "online",
		"uptime_sec": int64(time.Since(d.Started).Seconds()),
		"version":    Version,
	})
}

func (d Deps) handleListInstances(w http.ResponseWriter, _ *http.Request) {
	list, err := d.Store.List()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []store.Instance{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (d Deps) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		Transport string `json:"transport"`
		RoomID    string `json:"room_id"`
		KeyHex    string `json:"key_hex"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	inst, err := d.Store.Create(store.CreateInput{
		Name:      body.Name,
		Provider:  body.Provider,
		Transport: body.Transport,
		RoomID:    body.RoomID,
		KeyHex:    body.KeyHex,
		Comment:   body.Comment,
	})
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (d Deps) handleUpdateInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	in := store.UpdateInput{}
	if raw, ok := body["name"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid name")
			return
		}
		in.Name = &v
	}
	if raw, ok := body["provider"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid provider")
			return
		}
		in.Provider = &v
	}
	if raw, ok := body["transport"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid transport")
			return
		}
		in.Transport = &v
	}
	if raw, ok := body["room_id"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid room_id")
			return
		}
		in.RoomID = &v
	}
	if raw, ok := body["key_hex"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid key_hex")
			return
		}
		in.KeyHex = &v
	}
	if raw, ok := body["comment"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid comment")
			return
		}
		in.Comment = &v
	}
	if raw, ok := body["enabled"]; ok {
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid enabled")
			return
		}
		in.Enabled = &v
	}
	if raw, ok := body["status"]; ok {
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid status")
			return
		}
		in.Status = &v
	}

	inst, err := d.Store.Update(id, in)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "instance not found")
			return
		}
		if errors.Is(err, store.ErrInvalid) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (d Deps) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := d.Store.Delete(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "instance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (d Deps) handleRestartInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := d.Store.Restart(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "instance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"action":   "restart",
		"instance": inst,
	})
}

func (d Deps) handleDeploy(w http.ResponseWriter, _ *http.Request) {
	deployed, err := d.Store.MarkDeployed()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"action":   "deploy",
		"deployed": deployed,
	})
}

func (d Deps) handleURI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := d.Store.Get(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "instance not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	uri, err := buildURI(inst.Provider, inst.Transport, inst.RoomID, inst.KeyHex, inst.Comment)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"uri":        uri,
		"instance_id": inst.ID,
	})
}

// buildURI returns olcrtc://{provider}?{transport}@{room}#{key}${comment}
func buildURI(provider, transport, room, key, comment string) (string, error) {
	provider = strings.TrimSpace(provider)
	transport = strings.TrimSpace(transport)
	room = strings.TrimSpace(room)
	key = strings.TrimSpace(key)
	comment = strings.TrimSpace(comment)
	if provider == "" || transport == "" || room == "" || key == "" {
		return "", fmt.Errorf("provider, transport, room and key are required")
	}
	var b strings.Builder
	b.Grow(len(provider) + len(transport) + len(room) + len(key) + len(comment) + 32)
	b.WriteString("olcrtc://")
	b.WriteString(provider)
	b.WriteByte('?')
	b.WriteString(transport)
	b.WriteByte('@')
	b.WriteString(escapeField(room))
	b.WriteByte('#')
	b.WriteString(key)
	if comment != "" {
		b.WriteByte('$')
		b.WriteString(escapeField(comment))
	}
	return b.String(), nil
}

func escapeField(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '#', '$', '@', '?', '<', '>', '%':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
