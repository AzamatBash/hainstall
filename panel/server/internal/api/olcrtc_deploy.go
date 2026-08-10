package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/opsagent"
	"github.com/azabash/hapanel/panel/internal/store"
	"github.com/google/uuid"
)

type olcDeployJob struct {
	ID       string
	Status   string // queued|running|succeeded|failed
	NodeID   string
	Created  time.Time
	Finished *time.Time

	mu       sync.Mutex
	messages []opsagent.Message
}

func (j *olcDeployJob) add(role, step, text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.messages = append(j.messages, opsagent.Message{
		At:   time.Now().UTC(),
		Role: role,
		Step: step,
		Text: text,
	})
}

func (j *olcDeployJob) Messages() []opsagent.Message {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]opsagent.Message, len(j.messages))
	copy(out, j.messages)
	return out
}

var olcDeployJobs sync.Map // id -> *olcDeployJob

func (s *Server) handleDeployOlcrtcSSH(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		Host        string `json:"host"`
		SSHUser     string `json:"ssh_user"`
		SSHPassword string `json:"ssh_password"`
		SSHPort     int    `json:"ssh_port"`
		AgentPort   int    `json:"agent_port"`
		Country     string `json:"country"`
		ProviderID  string `json:"provider_id"`
		AccountID   string `json:"provider_account_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Host = strings.TrimSpace(body.Host)
	body.SSHUser = strings.TrimSpace(body.SSHUser)
	if body.Name == "" || body.Host == "" || body.SSHPassword == "" {
		writeErr(w, http.StatusBadRequest, "укажите имя, IP VPS и SSH пароль")
		return
	}
	if body.SSHUser == "" {
		body.SSHUser = "root"
	}
	if body.SSHPort <= 0 {
		body.SSHPort = 22
	}
	if body.AgentPort <= 0 {
		body.AgentPort = 9201
	}
	body.Country = strings.ToUpper(strings.TrimSpace(body.Country))
	body.ProviderID = strings.TrimSpace(body.ProviderID)
	body.AccountID = strings.TrimSpace(body.AccountID)
	if body.Country != "" && !isCountryCode(body.Country) {
		writeErr(w, http.StatusBadRequest, "некорректный код страны")
		return
	}
	if body.ProviderID == "" {
		body.AccountID = ""
	}

	bin := strings.TrimSpace(os.Getenv("OLCNODE_BIN"))
	if bin == "" {
		bin = strings.TrimSpace(os.Getenv("OLCRTC_AGENT_BIN"))
	}
	if bin == "" {
		bin = "/tmp/olcnode"
	}
	if _, err := os.Stat(bin); err != nil {
		writeErr(w, http.StatusInternalServerError, "бинарник olcnode не найден: "+bin+" (задайте OLCNODE_BIN на панели)")
		return
	}

	job := &olcDeployJob{
		ID:      uuid.NewString(),
		Status:  "queued",
		Created: time.Now().UTC(),
	}
	olcDeployJobs.Store(job.ID, job)

	go s.runOlcrtcSSHDeploy(job, body.Name, body.Host, body.SSHUser, body.SSHPassword, body.SSHPort, body.AgentPort, body.Country, body.ProviderID, body.AccountID, bin)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"job": map[string]any{
			"id":         job.ID,
			"status":     job.Status,
			"created_at": job.Created.Format(time.RFC3339),
		},
	})
}

func (s *Server) handleOlcrtcDeployJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	v, ok := olcDeployJobs.Load(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "задача не найдена")
		return
	}
	job := v.(*olcDeployJob)
	m := map[string]any{
		"id":         job.ID,
		"status":     job.Status,
		"node_id":    job.NodeID,
		"created_at": job.Created.Format(time.RFC3339),
	}
	if job.Finished != nil {
		m["finished_at"] = job.Finished.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job":      m,
		"messages": job.Messages(),
	})
}

func (s *Server) runOlcrtcSSHDeploy(job *olcDeployJob, name, host, user, password string, sshPort, agentPort int, country, providerID, accountID, binPath string) {
	job.Status = "running"
	fail := func(step, msg string) {
		job.add("error", step, msg)
		job.Status = "failed"
		now := time.Now().UTC()
		job.Finished = &now
	}

	job.add("agent", "token", "Генерирую token…")
	token, err := generateOlcrtcToken()
	if err != nil {
		fail("token", err.Error())
		return
	}
	job.add("success", "token", "Token сгенерирован (хранится на панели и в olcnode)")

	agentURL := fmt.Sprintf("http://%s:%d", host, agentPort)
	n, err := s.store.CreateOlcrtcNode(name, agentURL, host, token, country, providerID, accountID)
	if err != nil {
		fail("db", err.Error())
		return
	}
	job.NodeID = n.ID
	job.add("success", "db", "Нода создана в панели: "+n.ID)

	job.add("agent", "ssh", fmt.Sprintf("SSH %s@%s:%d …", user, host, sshPort))
	sshClient, err := opsagent.DialSSH(host, user, password, sshPort, 25*time.Second)
	if err != nil {
		_, _ = s.store.DeleteOlcrtcNode(n.ID)
		fail("ssh", err.Error())
		return
	}
	defer sshClient.Close()
	job.add("success", "ssh", "SSH OK")

	job.add("agent", "upload", "Загружаю olcnode на VPS…")
	binData, err := os.ReadFile(binPath)
	if err != nil {
		fail("upload", "чтение бинарника: "+err.Error())
		return
	}
	remoteBin := "/opt/olcnode/olcnode"
	if err := sshClient.Upload(remoteBin, binData); err != nil {
		fail("upload", err.Error())
		return
	}
	job.add("success", "upload", "Бинарник: "+remoteBin)

	unit := fmt.Sprintf(`[Unit]
Description=olcnode (olcRTC agent for hapanel)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=OLCNODE_TOKEN=%s
Environment=OLCNODE_LISTEN=:%d
Environment=OLCNODE_STATE=/opt/olcnode/state.json
Environment=OLCNODE_NAME=%s
ExecStart=%s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, token, agentPort, name, remoteBin)

	job.add("agent", "systemd", "Пишу systemd unit…")
	if err := sshClient.WriteFile("/etc/systemd/system/olcnode.service", []byte(unit)); err != nil {
		fail("systemd", err.Error())
		return
	}

	setup := fmt.Sprintf(`set -e
mkdir -p /opt/olcnode
chmod 755 /opt/olcnode /opt/olcnode/olcnode
systemctl daemon-reload
systemctl enable olcnode
systemctl restart olcnode
# open agent port if ufw active
if command -v ufw >/dev/null 2>&1; then
  ufw allow %d/tcp || true
fi
systemctl is-active olcnode
`, agentPort)
	job.add("agent", "start", "systemctl enable --now olcnode…")
	out, err := sshClient.Run(setup, 2*time.Minute)
	if err != nil {
		fail("start", err.Error()+"\n"+out)
		return
	}
	job.add("success", "start", strings.TrimSpace(out))

	job.add("agent", "health", "Жду health olcnode на VPS…")
	healthCmd := fmt.Sprintf(`for i in $(seq 1 30); do curl -fsS http://127.0.0.1:%d/_olcnode/v1/health && exit 0; sleep 1; done; echo timeout; exit 1`, agentPort)
	hout, err := sshClient.Run(healthCmd, 90*time.Second)
	if err != nil {
		// legacy prefix fallback
		healthCmd = fmt.Sprintf(`for i in $(seq 1 10); do curl -fsS http://127.0.0.1:%d/_olcrtc/v1/health && exit 0; sleep 1; done; echo timeout; exit 1`, agentPort)
		hout, err = sshClient.Run(healthCmd, 30*time.Second)
		if err != nil {
			fail("health", err.Error()+"\n"+hout)
			return
		}
	}
	job.add("success", "health", "localhost health OK")

	if s.olcrtc != nil {
		job.add("agent", "connect", "Панель → olcnode "+agentURL)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.olcrtc.WaitHealthy(ctx, agentURL, 15*time.Second); err != nil {
			job.add("error", "connect", "панель не достучалась снаружи: "+err.Error()+" (проверь firewall "+strconv.Itoa(agentPort)+"/tcp)")
			_, _ = s.store.SetOlcrtcNodeStatus(n.ID, store.OlcrtcStatusDegraded, err.Error(), time.Now().UTC().Unix())
		} else {
			_ = s.olcrtc.Deploy(ctx, agentURL, token)
			_, _ = s.store.SetOlcrtcNodeStatus(n.ID, store.OlcrtcStatusOnline, "", time.Now().UTC().Unix())
			job.add("success", "connect", "Связь панель → olcnode OK")
		}
	}

	job.add("success", "", "Готово. olcnode на "+host+" — управляй нодой из вкладки «Ноды».")
	job.Status = "succeeded"
	now := time.Now().UTC()
	job.Finished = &now
}
