package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/azabash/hapanel/panel/internal/olcnode"
	"github.com/azabash/hapanel/panel/internal/store"
)

var olcrtcLocalPIDs sync.Map // nodeID -> pid

func generateOlcrtcToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) publicOlcrtcNode(n store.OlcrtcNode) map[string]any {
	pub := store.OlcrtcNodePublic(n)
	m := map[string]any{
		"id":                     pub.ID,
		"name":                   pub.Name,
		"agent_url":              pub.AgentURL,
		"host":                   pub.Host,
		"country":                pub.Country,
		"provider_id":            pub.ProviderID,
		"provider_name":          "",
		"provider_favicon":       "",
		"provider_login_url":     "",
		"provider_account_id":    pub.ProviderAccountID,
		"provider_account_login": "",
		"has_token":              pub.HasToken,
		"status":                 pub.Status,
		"last_error":             pub.LastError,
		"last_seen_at":           pub.LastSeenAt,
		"created_at":             pub.CreatedAt,
		"updated_at":             pub.UpdatedAt,
	}
	if n.Token != "" {
		m["token"] = n.Token
	}
	if pub.ProviderID != "" {
		if p, err := s.store.GetProvider(pub.ProviderID); err == nil && p != nil {
			m["provider_name"] = p.Name
			m["provider_favicon"] = p.FaviconURL
			m["provider_login_url"] = p.LoginURL
		}
	}
	if pub.ProviderAccountID != "" {
		if a, err := s.store.GetProviderAccount(pub.ProviderAccountID); err == nil && a != nil {
			m["provider_account_login"] = a.Login
		}
	}
	return m
}

func (s *Server) handleListOlcrtcNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListOlcrtcNodes()
	if err != nil {
		s.logger.Error("list olcrtc nodes", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, s.publicOlcrtcNode(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

func (s *Server) handleCreateOlcrtcNode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name              string `json:"name"`
		AgentURL          string `json:"agent_url"`
		Host              string `json:"host"`
		Token             string `json:"token"`
		Country           string `json:"country"`
		ProviderID        string `json:"provider_id"`
		ProviderAccountID string `json:"provider_account_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.AgentURL = strings.TrimRight(strings.TrimSpace(body.AgentURL), "/")
	body.Host = strings.TrimSpace(body.Host)
	body.Token = strings.TrimSpace(body.Token)
	body.Country = strings.ToUpper(strings.TrimSpace(body.Country))
	body.ProviderID = strings.TrimSpace(body.ProviderID)
	body.ProviderAccountID = strings.TrimSpace(body.ProviderAccountID)
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя")
		return
	}
	if body.Country != "" && !isCountryCode(body.Country) {
		writeErr(w, http.StatusBadRequest, "некорректный код страны")
		return
	}
	if body.ProviderID == "" {
		body.ProviderAccountID = ""
	}
	if body.Token == "" {
		tok, err := generateOlcrtcToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "не удалось сгенерировать token")
			return
		}
		body.Token = tok
	}
	n, err := s.store.CreateOlcrtcNode(body.Name, body.AgentURL, body.Host, body.Token, body.Country, body.ProviderID, body.ProviderAccountID)
	if err != nil {
		s.logger.Error("create olcrtc node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"node": s.publicOlcrtcNode(*n), "token": n.Token})
}

func (s *Server) handleGetOlcrtcNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetOlcrtcNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	// MVP local: include token in detail (TODO secretbox).
	writeJSON(w, http.StatusOK, s.publicOlcrtcNode(*n))
}

func (s *Server) handleUpdateOlcrtcNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name              *string `json:"name"`
		AgentURL          *string `json:"agent_url"`
		Host              *string `json:"host"`
		Token             *string `json:"token"`
		Country           *string `json:"country"`
		ProviderID        *string `json:"provider_id"`
		ProviderAccountID *string `json:"provider_account_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}

	metaOnly := body.Name == nil && body.AgentURL == nil && body.Host == nil && body.Token == nil
	if metaOnly && (body.Country != nil || body.ProviderID != nil || body.ProviderAccountID != nil) {
		cur, err := s.store.GetOlcrtcNode(id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
		if cur == nil {
			writeErr(w, http.StatusNotFound, "нода не найдена")
			return
		}
		n := cur
		if body.Country != nil {
			c := strings.ToUpper(strings.TrimSpace(*body.Country))
			if c != "" && !isCountryCode(c) {
				writeErr(w, http.StatusBadRequest, "некорректный код страны")
				return
			}
			n, err = s.store.UpdateOlcrtcNodeCountry(id, c)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
		}
		if body.ProviderID != nil || body.ProviderAccountID != nil {
			prov := cur.ProviderID
			acc := cur.ProviderAccountID
			if body.ProviderID != nil {
				prov = strings.TrimSpace(*body.ProviderID)
			}
			if body.ProviderAccountID != nil {
				acc = strings.TrimSpace(*body.ProviderAccountID)
			}
			if body.ProviderID != nil && body.ProviderAccountID == nil && prov != cur.ProviderID {
				acc = ""
			}
			n, err = s.store.UpdateOlcrtcNodeProvider(id, prov, acc)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
				return
			}
		}
		if n == nil {
			writeErr(w, http.StatusNotFound, "нода не найдена")
			return
		}
		writeJSON(w, http.StatusOK, s.publicOlcrtcNode(*n))
		return
	}

	cur, err := s.store.GetOlcrtcNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if cur == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	name, agentURL, host, token := cur.Name, cur.AgentURL, cur.Host, ""
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	if body.AgentURL != nil {
		agentURL = strings.TrimRight(strings.TrimSpace(*body.AgentURL), "/")
	}
	if body.Host != nil {
		host = strings.TrimSpace(*body.Host)
	}
	if body.Token != nil {
		token = strings.TrimSpace(*body.Token)
	}
	n, err := s.store.UpdateOlcrtcNode(id, name, agentURL, host, token)
	if err != nil {
		s.logger.Error("update olcrtc node", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if body.Country != nil {
		c := strings.ToUpper(strings.TrimSpace(*body.Country))
		if c != "" && !isCountryCode(c) {
			writeErr(w, http.StatusBadRequest, "некорректный код страны")
			return
		}
		n, err = s.store.UpdateOlcrtcNodeCountry(id, c)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	if body.ProviderID != nil || body.ProviderAccountID != nil {
		prov := cur.ProviderID
		acc := cur.ProviderAccountID
		if body.ProviderID != nil {
			prov = strings.TrimSpace(*body.ProviderID)
		}
		if body.ProviderAccountID != nil {
			acc = strings.TrimSpace(*body.ProviderAccountID)
		}
		if body.ProviderID != nil && body.ProviderAccountID == nil && prov != cur.ProviderID {
			acc = ""
		}
		n, err = s.store.UpdateOlcrtcNodeProvider(id, prov, acc)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
			return
		}
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	writeJSON(w, http.StatusOK, s.publicOlcrtcNode(*n))
}

func (s *Server) handleDeleteOlcrtcNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ok, err := s.store.DeleteOlcrtcNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if v, loaded := olcrtcLocalPIDs.LoadAndDelete(id); loaded {
		if pid, ok := v.(int); ok && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleOlcrtcNodeRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetOlcrtcNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	ctx := r.Context()
	if n.AgentURL == "" || n.Token == "" || s.olcrtc == nil {
		writeErr(w, http.StatusBadRequest, "у ноды нет agent_url/token")
		return
	}
	insts, listErr := s.olcrtc.ListInstances(ctx, n.AgentURL, n.Token)
	if listErr != nil {
		_ = s.markOlcrtcNodeOffline(id, listErr)
		writeAgentErr(w, listErr)
		return
	}
	var restartErrs []string
	for _, inst := range insts {
		if _, err := s.olcrtc.RestartInstance(ctx, n.AgentURL, n.Token, inst.ID); err != nil {
			restartErrs = append(restartErrs, inst.ID+": "+err.Error())
		}
	}
	st, err := s.olcrtc.Status(ctx, n.AgentURL, n.Token)
	if err != nil {
		_ = s.markOlcrtcNodeOffline(id, err)
		writeAgentErr(w, err)
		return
	}
	status := store.OlcrtcStatusOnline
	if st.Status != "" {
		status = st.Status
	}
	now := time.Now().UTC().Unix()
	lastErr := ""
	if len(restartErrs) > 0 {
		lastErr = strings.Join(restartErrs, "; ")
		status = store.OlcrtcStatusDegraded
	}
	n, err = s.store.SetOlcrtcNodeStatus(id, status, lastErr, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "action": "restart", "node": n})
}

func (s *Server) handleOlcrtcNodeRefreshStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := s.store.GetOlcrtcNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if n.AgentURL == "" || s.olcrtc == nil {
		writeErr(w, http.StatusBadRequest, "у ноды нет agent_url")
		return
	}
	ctx := r.Context()
	now := time.Now().UTC().Unix()

	_, herr := s.olcrtc.Health(ctx, n.AgentURL)
	if herr != nil {
		n, _ = s.store.SetOlcrtcNodeStatus(id, store.OlcrtcStatusOffline, herr.Error(), now)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
		return
	}
	if n.Token == "" {
		n, _ = s.store.SetOlcrtcNodeStatus(id, store.OlcrtcStatusDegraded, "нет token", now)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
		return
	}
	st, err := s.olcrtc.Status(ctx, n.AgentURL, n.Token)
	if err != nil {
		n, _ = s.store.SetOlcrtcNodeStatus(id, store.OlcrtcStatusOffline, err.Error(), now)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n})
		return
	}
	status := store.OlcrtcStatusOnline
	if st.Status != "" {
		status = st.Status
	}
	n, err = s.store.SetOlcrtcNodeStatus(id, status, st.LastError, now)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "node": n, "agent_status": st})
}

func (s *Server) handleDeployOlcrtcLocal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Host = strings.TrimSpace(body.Host)
	if body.Name == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя")
		return
	}
	if body.Host == "" {
		body.Host = "127.0.0.1"
	}
	port := body.Port
	if port <= 0 {
		p, err := pickFreeListenPort(9201)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		port = p
	} else if !portFree(port) {
		writeErr(w, http.StatusConflict, fmt.Sprintf("порт %d занят", port))
		return
	}

	token, err := generateOlcrtcToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "не удалось сгенерировать token")
		return
	}
	agentURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	n, err := s.store.CreateOlcrtcNode(body.Name, agentURL, body.Host, token, "", "", "")
	if err != nil {
		s.logger.Error("create olcrtc node (deploy-local)", "err", err)
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}

	bin := strings.TrimSpace(os.Getenv("OLCNODE_BIN"))
	if bin == "" {
		bin = strings.TrimSpace(os.Getenv("OLCRTC_AGENT_BIN"))
	}
	if bin == "" {
		bin = "/tmp/olcnode"
	}
	if _, err := os.Stat(bin); err != nil {
		_, _ = s.store.DeleteOlcrtcNode(n.ID)
		writeErr(w, http.StatusInternalServerError, "бинарник olcnode не найден: "+bin+" (задайте OLCNODE_BIN)")
		return
	}

	statePath, err := filepath.Abs(filepath.Join("data", fmt.Sprintf("olcnode-%s.json", n.ID)))
	if err != nil {
		_, _ = s.store.DeleteOlcrtcNode(n.ID)
		writeErr(w, http.StatusInternalServerError, "не удалось выбрать STATE path")
		return
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		_, _ = s.store.DeleteOlcrtcNode(n.ID)
		writeErr(w, http.StatusInternalServerError, "не удалось создать data/")
		return
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"OLCNODE_TOKEN="+token,
		"OLCNODE_LISTEN=:"+strconv.Itoa(port),
		"OLCNODE_STATE="+statePath,
		"OLCNODE_NAME="+body.Name,
	)
	logPath := filepath.Join(filepath.Dir(statePath), fmt.Sprintf("olcnode-%s.log", n.ID))
	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		cmd.Stdout = logF
		cmd.Stderr = logF
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		_, _ = s.store.DeleteOlcrtcNode(n.ID)
		writeErr(w, http.StatusInternalServerError, "не удалось запустить olcnode: "+err.Error())
		return
	}
	olcrtcLocalPIDs.Store(n.ID, cmd.Process.Pid)
	go func() {
		_ = cmd.Wait()
		if logF != nil {
			_ = logF.Close()
		}
		olcrtcLocalPIDs.Delete(n.ID)
	}()

	ctx := r.Context()
	if s.olcrtc == nil {
		writeErr(w, http.StatusInternalServerError, "olcnode client не настроен")
		return
	}
	if err := s.olcrtc.WaitHealthy(ctx, agentURL, 5*time.Second); err != nil {
		_, _ = s.store.SetOlcrtcNodeStatus(n.ID, store.OlcrtcStatusOffline, err.Error(), time.Now().UTC().Unix())
		writeErr(w, http.StatusBadGateway, "olcnode не ответил на health: "+err.Error())
		return
	}
	if err := s.olcrtc.Deploy(ctx, agentURL, token); err != nil {
		_, _ = s.store.SetOlcrtcNodeStatus(n.ID, store.OlcrtcStatusDegraded, err.Error(), time.Now().UTC().Unix())
		writeErr(w, http.StatusBadGateway, "olcnode deploy: "+err.Error())
		return
	}
	now := time.Now().UTC().Unix()
	nodeID := n.ID
	if updated, err := s.store.SetOlcrtcNodeStatus(nodeID, store.OlcrtcStatusOnline, "", now); err == nil && updated != nil {
		n = updated
	}
	if n != nil && n.Token == "" {
		n.Token = token
		n.HasToken = true
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"node":  n,
		"token": token,
		"pid":   cmd.Process.Pid,
		"port":  port,
	})
}

func (s *Server) handleListOlcrtcNodeInstances(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := s.store.GetOlcrtcNode(nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if n.AgentURL == "" || n.Token == "" || s.olcrtc == nil {
		writeErr(w, http.StatusBadRequest, "у ноды нет agent_url/token")
		return
	}
	insts, err := s.olcrtc.ListInstances(r.Context(), n.AgentURL, n.Token)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": mapAgentInstances(nodeID, insts)})
}

func (s *Server) handleCreateOlcrtcInstance(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("id")
	n, err := s.store.GetOlcrtcNode(nodeID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "ошибка базы данных")
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "нода не найдена")
		return
	}
	if n.AgentURL == "" || n.Token == "" || s.olcrtc == nil {
		writeErr(w, http.StatusBadRequest, "у ноды нет agent_url/token")
		return
	}
	var body struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		Transport string `json:"transport"`
		RoomID    string `json:"room_id"`
		KeyHex    string `json:"key_hex"`
		Comment   string `json:"comment"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Provider = strings.TrimSpace(body.Provider)
	body.Transport = strings.TrimSpace(body.Transport)
	body.RoomID = strings.TrimSpace(body.RoomID)
	body.KeyHex = strings.TrimSpace(body.KeyHex)
	body.Comment = strings.TrimSpace(body.Comment)
	if body.Name == "" || body.Provider == "" || body.Transport == "" || body.RoomID == "" {
		writeErr(w, http.StatusBadRequest, "укажите name, provider, transport и room_id")
		return
	}
	if body.KeyHex == "" {
		tok, err := generateOlcrtcToken()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "не удалось сгенерировать key")
			return
		}
		body.KeyHex = tok
	}
	if !store.ValidOlcrtcProvider(body.Provider) {
		writeErr(w, http.StatusBadRequest, "некорректный provider")
		return
	}
	if !store.ValidOlcrtcTransport(body.Transport) {
		writeErr(w, http.StatusBadRequest, "некорректный transport")
		return
	}
	if !store.CompatibleOlcrtcPair(body.Provider, body.Transport) {
		writeErr(w, http.StatusBadRequest, "transport не совместим с provider")
		return
	}
	if err := store.ValidateOlcrtcKeyHex(body.KeyHex); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	inst, err := s.olcrtc.CreateInstance(r.Context(), n.AgentURL, n.Token, olcnode.CreateInstanceRequest{
		Name: body.Name, Provider: body.Provider, Transport: body.Transport,
		RoomID: body.RoomID, KeyHex: body.KeyHex, Comment: body.Comment,
	})
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapAgentInstance(nodeID, *inst))
}

func (s *Server) handleUpdateOlcrtcInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		Transport string `json:"transport"`
		RoomID    string `json:"room_id"`
		KeyHex    string `json:"key_hex"`
		Comment   string `json:"comment"`
		Enabled   *bool  `json:"enabled"`
		NodeID    string `json:"node_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Provider = strings.TrimSpace(body.Provider)
	body.Transport = strings.TrimSpace(body.Transport)
	body.RoomID = strings.TrimSpace(body.RoomID)
	body.KeyHex = strings.TrimSpace(body.KeyHex)
	body.Comment = strings.TrimSpace(body.Comment)
	if body.Provider != "" && !store.ValidOlcrtcProvider(body.Provider) {
		writeErr(w, http.StatusBadRequest, "некорректный provider")
		return
	}
	if body.Transport != "" && !store.ValidOlcrtcTransport(body.Transport) {
		writeErr(w, http.StatusBadRequest, "некорректный transport")
		return
	}
	if body.Provider != "" && body.Transport != "" && !store.CompatibleOlcrtcPair(body.Provider, body.Transport) {
		writeErr(w, http.StatusBadRequest, "transport не совместим с provider")
		return
	}
	if body.KeyHex != "" {
		if err := store.ValidateOlcrtcKeyHex(body.KeyHex); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	nodeID := strings.TrimSpace(body.NodeID)
	if nodeID == "" {
		nodeID = strings.TrimSpace(r.URL.Query().Get("node_id"))
	}
	n, err := s.resolveOlcrtcNodeForInstance(r, nodeID, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "инстанс не найден")
		return
	}
	inst, err := s.olcrtc.UpdateInstance(r.Context(), n.AgentURL, n.Token, id, olcnode.UpdateInstanceRequest{
		Name: body.Name, Provider: body.Provider, Transport: body.Transport,
		RoomID: body.RoomID, KeyHex: body.KeyHex, Comment: body.Comment, Enabled: body.Enabled,
	})
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mapAgentInstance(n.ID, *inst))
}

func (s *Server) handleDeleteOlcrtcInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	n, err := s.resolveOlcrtcNodeForInstance(r, nodeID, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "инстанс не найден")
		return
	}
	if err := s.olcrtc.DeleteInstance(r.Context(), n.AgentURL, n.Token, id); err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRestartOlcrtcInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	n, err := s.resolveOlcrtcNodeForInstance(r, nodeID, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "инстанс не найден")
		return
	}
	inst, err := s.olcrtc.RestartInstance(r.Context(), n.AgentURL, n.Token, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	if inst == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "instance": mapAgentInstance(n.ID, *inst)})
}

func (s *Server) handleOlcrtcInstanceURI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	n, err := s.resolveOlcrtcNodeForInstance(r, nodeID, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	if n == nil {
		writeErr(w, http.StatusNotFound, "инстанс не найден")
		return
	}
	uri, err := s.olcrtc.InstanceURI(r.Context(), n.AgentURL, n.Token, id)
	if err != nil {
		writeAgentErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, uri)
}

func (s *Server) resolveOlcrtcNodeForInstance(r *http.Request, nodeID, instanceID string) (*store.OlcrtcNode, error) {
	if s.olcrtc == nil {
		return nil, errors.New("olcrtc client не настроен")
	}
	ctx := r.Context()
	if nodeID != "" {
		n, err := s.store.GetOlcrtcNode(nodeID)
		if err != nil {
			return nil, err
		}
		if n == nil || n.AgentURL == "" || n.Token == "" {
			return nil, nil
		}
		return n, nil
	}
	nodes, err := s.store.ListOlcrtcNodes()
	if err != nil {
		return nil, err
	}
	for i := range nodes {
		n := &nodes[i]
		if n.AgentURL == "" || n.Token == "" {
			continue
		}
		insts, err := s.olcrtc.ListInstances(ctx, n.AgentURL, n.Token)
		if err != nil {
			continue
		}
		for _, inst := range insts {
			if inst.ID == instanceID {
				return n, nil
			}
		}
	}
	return nil, nil
}

func (s *Server) markOlcrtcNodeOffline(id string, cause error) *store.OlcrtcNode {
	msg := ""
	if cause != nil {
		msg = cause.Error()
	}
	n, _ := s.store.SetOlcrtcNodeStatus(id, store.OlcrtcStatusOffline, msg, time.Now().UTC().Unix())
	return n
}

func mapAgentInstances(nodeID string, insts []olcnode.Instance) []map[string]any {
	out := make([]map[string]any, 0, len(insts))
	for _, inst := range insts {
		out = append(out, mapAgentInstance(nodeID, inst))
	}
	return out
}

func mapAgentInstance(nodeID string, inst olcnode.Instance) map[string]any {
	return map[string]any{
		"id":         inst.ID,
		"node_id":    nodeID,
		"name":       inst.Name,
		"provider":   inst.Provider,
		"transport":  inst.Transport,
		"room_id":    inst.RoomID,
		"key_hex":    inst.KeyHex,
		"comment":    inst.Comment,
		"enabled":    inst.Enabled,
		"status":     inst.Status,
		"created_at": inst.CreatedAt,
		"updated_at": inst.UpdatedAt,
	}
}

func writeAgentErr(w http.ResponseWriter, err error) {
	if err == nil {
		writeErr(w, http.StatusBadGateway, "агент недоступен")
		return
	}
	var ae *olcnode.APIError
	if errors.As(err, &ae) {
		status := http.StatusBadGateway
		if ae.Status >= 400 && ae.Status < 500 {
			status = ae.Status
		}
		writeErr(w, status, ae.Message)
		return
	}
	writeErr(w, http.StatusBadGateway, "агент недоступен: "+err.Error())
}

func pickFreeListenPort(start int) (int, error) {
	for p := start; p < start+200; p++ {
		if portFree(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("нет свободного порта начиная с %d", start)
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
