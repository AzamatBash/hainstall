package opsagent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/azabash/hapanel/panel/internal/provision"
	"github.com/azabash/hapanel/panel/internal/store"
	"github.com/google/uuid"
)

type Message struct {
	At      time.Time `json:"at"`
	Role    string    `json:"role"` // system|agent|user|error|success|llm
	Text    string    `json:"text"`
	Step    string    `json:"step,omitempty"`
}

type JobStatus string

const (
	StatusQueued    JobStatus = "queued"
	StatusRunning   JobStatus = "running"
	StatusSucceeded JobStatus = "succeeded"
	StatusFailed    JobStatus = "failed"
)

type Job struct {
	ID       string    `json:"id"`
	Status   JobStatus `json:"status"`
	NodeID   string    `json:"node_id,omitempty"`
	Created  time.Time `json:"created_at"`
	Finished *time.Time `json:"finished_at,omitempty"`

	mu       sync.Mutex
	messages []Message
}

func (j *Job) Messages() []Message {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]Message, len(j.messages))
	copy(out, j.messages)
	return out
}

func (j *Job) add(role, step, text string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.messages = append(j.messages, Message{
		At:   time.Now().UTC(),
		Role: role,
		Step: step,
		Text: text,
	})
}

type DeployRequest struct {
	Name          string
	Host          string
	SSHUser       string
	SSHPassword   string
	SSHPort       int
	MgmtPort      int
	PanelIP       string
	KeepRemnanode bool
}

type Runner struct {
	Store       *store.Store
	LLM         *LLM
	Logger      *slog.Logger
	PanelIP     string
	OnNodeReady func(nodeID string) // optional: panel→agent connect

	mu   sync.Mutex
	jobs map[string]*Job
}

func NewRunner(st *store.Store, llm *LLM, panelIP string, logger *slog.Logger) *Runner {
	return &Runner{
		Store:   st,
		LLM:     llm,
		Logger:  logger,
		PanelIP: strings.TrimSpace(panelIP),
		jobs:    make(map[string]*Job),
	}
}

func (r *Runner) GetJob(id string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

func (r *Runner) StartDeploy(req DeployRequest) (*Job, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Host = strings.TrimSpace(req.Host)
	req.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.Name == "" || req.Host == "" || req.SSHUser == "" || req.SSHPassword == "" {
		return nil, fmt.Errorf("укажите name, host, ssh_user, ssh_password")
	}
	if req.SSHPort <= 0 {
		req.SSHPort = 22
	}
	if req.MgmtPort <= 0 {
		req.MgmtPort = provision.DefaultMgmtPort
	}
	if req.PanelIP == "" {
		req.PanelIP = r.PanelIP
	}

	job := &Job{
		ID:      uuid.NewString(),
		Status:  StatusQueued,
		Created: time.Now().UTC(),
	}
	job.add("system", "", fmt.Sprintf("Задача создана: нода %q на %s", req.Name, req.Host))
	r.mu.Lock()
	r.jobs[job.ID] = job
	r.mu.Unlock()

	go r.runDeploy(job, req)
	return job, nil
}

func (r *Runner) runDeploy(job *Job, req DeployRequest) {
	job.Status = StatusRunning
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	fail := func(step, msg string) {
		job.add("error", step, msg)
		job.Status = StatusFailed
		now := time.Now().UTC()
		job.Finished = &now
	}

	// 1) Provision in panel DB
	job.add("agent", "provision", "Создаю ноду в панели и генерирую compose…")
	bundle, err := provision.Generate(req.Name, req.Host, req.MgmtPort, "")
	if err != nil {
		fail("provision", err.Error())
		return
	}
	n, err := r.Store.CreateNode(req.Name, bundle.URL, bundle.Token)
	if err != nil {
		fail("provision", "ошибка БД: "+err.Error())
		return
	}
	_ = r.Store.UpdateNodeStatus(n.ID, store.StatusUnknown, nil)
	job.NodeID = n.ID
	job.add("success", "provision", "Нода создана: "+n.ID)

	// 2) SSH
	job.add("agent", "ssh", fmt.Sprintf("Подключаюсь по SSH %s@%s:%d …", req.SSHUser, req.Host, req.SSHPort))
	sshClient, err := DialSSH(req.Host, req.SSHUser, req.SSHPassword, req.SSHPort, 25*time.Second)
	if err != nil {
		if !r.recoverSSH(ctx, job, req, &sshClient, "ssh", err.Error()) {
			fail("ssh", err.Error())
			return
		}
	}
	defer sshClient.Close()
	job.add("success", "ssh", "SSH OK")

	hostOS, _ := sshClient.Run("cat /etc/os-release 2>/dev/null | head -n 5; uname -a", 30*time.Second)
	job.add("agent", "ssh", "Хост:\n"+truncate(hostOS, 500))

	runStep := func(step, desc, cmd string, timeout time.Duration) bool {
		job.add("agent", step, desc)
		out, err := sshClient.Run(cmd, timeout)
		if err == nil {
			if strings.TrimSpace(out) != "" {
				job.add("agent", step, truncate(out, 1500))
			}
			job.add("success", step, "OK")
			return true
		}
		job.add("error", step, err.Error())
		return r.recoverAndRetry(ctx, job, sshClient, hostOS, step, err.Error(), cmd, timeout)
	}

	// 3) Docker
	if !runStep("docker", "Проверяю/ставлю Docker…", dockerInstallScript(), 10*time.Minute) {
		fail("docker", "не удалось установить Docker")
		return
	}

	// 3b) Optional remnanode detect + wire HAProxy backend via docker network
	if req.KeepRemnanode {
		job.add("agent", "remnanode", "Режим «оставить remnanode» — ищу контейнер…")
		out, err := sshClient.Run(detectRemnanodeScript(), 2*time.Minute)
		if err != nil {
			job.add("agent", "remnanode", "detect: "+truncate(err.Error(), 500))
		} else if strings.TrimSpace(out) != "" {
			job.add("agent", "remnanode", truncate(out, 800))
		}
		det := parseRemnanodeDetect(out)
		if det.Found {
			patchBundleForRemnanode(&bundle, det.Container, det.Network)
			job.add("success", "remnanode", fmt.Sprintf("Бэкенд app → %s:8443 (сеть %s)", det.Container, det.Network))
			if det.Host8443 && det.Warn != "" {
				job.add("agent", "remnanode", "⚠ "+det.Warn)
			}
		} else {
			job.add("agent", "remnanode", "Контейнер remnanode не найден — HAProxy без бэкенда, добавьте серверы в панели")
		}
	}

	conflictsDesc := "Останавливаю remnanode/nginx если мешают портам 80/8443…"
	if req.KeepRemnanode {
		conflictsDesc = "Останавливаю nginx (remnanode не трогаю)…"
	}
	if !runStep("conflicts", conflictsDesc, clearConflictsScript(req.KeepRemnanode), 5*time.Minute) {
		job.add("agent", "conflicts", "Конфликты не сняты полностью — пробую дальше")
	}

	// 4) Firewall + fail2ban
	panelIP := strings.TrimSpace(req.PanelIP)
	fw := hardenScript(panelIP, req.MgmtPort)
	if !runStep("harden", "UFW + fail2ban (sshd)…", fw, 5*time.Minute) {
		job.add("agent", "harden", "Харденинг не обязателен — продолжаю")
	}

	// 5) Write files
	job.add("agent", "files", "Пишу /opt/hapanel-node …")
	base := "/opt/hapanel-node"
	for path, content := range bundle.Files {
		remote := base + "/" + path
		if err := sshClient.WriteFile(remote, []byte(content)); err != nil {
			job.add("error", "files", path+": "+err.Error())
			_ = r.recoverAndRetry(ctx, job, sshClient, hostOS, "files", err.Error(), "", 2*time.Minute)
			if err2 := sshClient.WriteFile(remote, []byte(content)); err2 != nil {
				fail("files", err2.Error())
				return
			}
		}
	}
	_, _ = sshClient.Run(fmt.Sprintf("mkdir -p %s/certs && chmod 600 %s/.env 2>/dev/null || true", base, base), time.Minute)
	job.add("success", "files", "Файлы записаны")

	// 6) Compose up
	up := fmt.Sprintf(`set -e
cd %s
DOCKER_GID=$(stat -c '%%g' /var/run/docker.sock 2>/dev/null || echo 0)
if grep -q '^DOCKER_GID=' .env 2>/dev/null; then
  sed -i "s/^DOCKER_GID=.*/DOCKER_GID=$DOCKER_GID/" .env
else
  echo DOCKER_GID=$DOCKER_GID >> .env
fi
export DOCKER_GID
docker compose pull || true
if ! docker compose up -d; then
  echo "=== compose ps ==="
  docker compose ps -a || true
  echo "=== compose logs ==="
  docker compose logs --tail 100 || true
  exit 1
fi
docker compose ps
`, base)
	if !runStep("compose", "docker compose up -d …", up, 12*time.Minute) {
		fail("compose", "не удалось поднять compose")
		return
	}

	// 7) Local health on VPS
	health := fmt.Sprintf(`for i in $(seq 1 30); do curl -fsS http://127.0.0.1:%d/_hapctl/v1/health && exit 0; sleep 2; done; echo health_timeout; exit 1`, req.MgmtPort)
	if !runStep("health", "Жду health агента на VPS…", health, 2*time.Minute) {
		fail("health", "агент не отвечает на localhost")
		return
	}

	if r.OnNodeReady != nil {
		job.add("agent", "connect", "Проверяю связь панель → агент…")
		r.OnNodeReady(n.ID)
		job.add("success", "connect", "Проверка связи выполнена")
	}

	job.add("success", "", fmt.Sprintf("Готово. Нода %s в панели.", n.ID))
	job.Status = StatusSucceeded
	now := time.Now().UTC()
	job.Finished = &now
}

func (r *Runner) recoverSSH(ctx context.Context, job *Job, req DeployRequest, out **SSHClient, step, errText string) bool {
	if r.LLM == nil || !r.LLM.Enabled() {
		job.add("agent", step, "LLM недоступен — автопочинка SSH пропущена")
		return false
	}
	job.add("llm", step, "Playbook не смог подключиться — спрашиваю LLM…")
	rec, err := r.LLM.SuggestRecovery(ctx, step, errText, "unknown")
	if err != nil {
		job.add("error", step, "LLM: "+err.Error())
		return false
	}
	if rec.Note != "" {
		job.add("llm", step, rec.Note)
	}
	// Can't run remote cmds without SSH; LLM note only. Retry dial once.
	job.add("agent", step, "Повторный SSH…")
	c, err := DialSSH(req.Host, req.SSHUser, req.SSHPassword, req.SSHPort, 25*time.Second)
	if err != nil {
		job.add("error", step, err.Error())
		return false
	}
	*out = c
	return true
}

func (r *Runner) recoverAndRetry(ctx context.Context, job *Job, ssh *SSHClient, hostOS, step, errText, originalCmd string, timeout time.Duration) bool {
	if r.LLM == nil || !r.LLM.Enabled() {
		job.add("agent", step, "LLM недоступен — останавливаюсь на ошибке playbook")
		return false
	}
	job.add("llm", step, "Playbook ошибся — дергаю LLM API…")
	rec, err := r.LLM.SuggestRecovery(ctx, step+"\noriginal: "+originalCmd, errText, hostOS)
	if err != nil {
		job.add("error", step, "LLM: "+err.Error())
		return false
	}
	if rec.Note != "" {
		job.add("llm", step, rec.Note)
	}
	for i, cmd := range rec.Commands {
		job.add("llm", step, fmt.Sprintf("LLM cmd %d/%d: %s", i+1, len(rec.Commands), cmd))
		out, err := ssh.Run(cmd, timeout)
		if err != nil {
			job.add("error", step, "LLM cmd failed: "+err.Error())
			return false
		}
		if strings.TrimSpace(out) != "" {
			job.add("agent", step, truncate(out, 1200))
		}
	}
	if originalCmd == "" {
		return true
	}
	job.add("agent", step, "Повторяю шаг playbook…")
	out, err := ssh.Run(originalCmd, timeout)
	if err != nil {
		job.add("error", step, "повтор: "+err.Error())
		return false
	}
	if strings.TrimSpace(out) != "" {
		job.add("agent", step, truncate(out, 1200))
	}
	job.add("success", step, "OK после LLM")
	return true
}

func dockerInstallScript() string {
	return `set -e
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  docker --version
  docker compose version
  exit 0
fi
export DEBIAN_FRONTEND=noninteractive
if command -v apt-get >/dev/null 2>&1; then
  apt-get update -y
  apt-get install -y ca-certificates curl
  curl -fsSL https://get.docker.com | sh
  systemctl enable --now docker || true
else
  echo "unsupported OS: need apt/docker"
  exit 1
fi
docker --version
docker compose version
`
}

func hardenScript(panelIP string, mgmtPort int) string {
	panelIP = strings.TrimSpace(panelIP)
	ufwMgmt := `echo "PANEL_IP empty — skip mgmt UFW rule"`
	if panelIP != "" {
		ufwMgmt = fmt.Sprintf(`ufw allow from %s to any port %d proto tcp || true`, panelIP, mgmtPort)
	}
	return fmt.Sprintf(`set -e
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y ufw fail2ban

echo "=== UFW ==="
ufw allow OpenSSH || ufw allow 22/tcp || true
ufw allow 8443/tcp || true
%s
# Non-interactive enable
ufw --force enable || true
ufw status verbose || true

echo "=== fail2ban sshd ==="
mkdir -p /etc/fail2ban
cat > /etc/fail2ban/jail.local <<'JAIL'
[DEFAULT]
bantime  = 1h
findtime = 10m
maxretry = 5
backend  = systemd

[sshd]
enabled = true
port    = ssh
filter  = sshd
maxretry = 5
JAIL
systemctl enable --now fail2ban
systemctl restart fail2ban
fail2ban-client status sshd 2>/dev/null || fail2ban-client status || true
echo "harden done"
`, ufwMgmt)
}
