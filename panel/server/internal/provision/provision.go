package provision

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// AgentImage is the Docker Hub image for the node agent.
// Also published as azamatbash/hanode:latest — prefer the version tag in production.
const AgentImage = "azamatbash/hanode:0.1.0"

// DefaultMgmtPort is the panel↔agent HTTP port (clients stay on 8443).
const DefaultMgmtPort = 47893

// Bundle is everything the operator copies onto the VPS.
type Bundle struct {
	Token      string            `json:"token"`
	URL        string            `json:"url"`
	Host       string            `json:"host"`
	Port       int               `json:"port"`
	Files      map[string]string `json:"files"`
	Commands   string            `json:"commands"`
	AgentImage string            `json:"agent_image"`
}

// NewToken returns a 32-byte hex token.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// BuildURL builds the panel→node management base URL (plain HTTP to agent).
// Port is the management port (default 47893). Always includes :port unless 80.
func BuildURL(host string, port int) string {
	host = normalizeHost(host)
	if port <= 0 {
		port = DefaultMgmtPort
	}
	if port == 80 {
		return "http://" + host
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

// ParseURL extracts host and management port from a node base URL.
func ParseURL(raw string) (host string, port int, err error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0, err
	}
	host = u.Hostname()
	if host == "" {
		return "", 0, fmt.Errorf("missing host")
	}
	port = DefaultMgmtPort
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil {
			return "", 0, err
		}
	}
	return host, port, nil
}

func normalizeHost(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	// strip accidental :port from host field
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// Generate creates install files for a node.
// port is the host management port published on the agent (panel → agent HTTP).
// HAProxy only serves clients on 8443 — no /_hapctl routing.
func Generate(name, host string, port int, token string) (Bundle, error) {
	host = normalizeHost(host)
	if host == "" {
		return Bundle{}, fmt.Errorf("host is required")
	}
	if port <= 0 {
		port = DefaultMgmtPort
	}
	if port > 65535 {
		return Bundle{}, fmt.Errorf("port must be 1–65535")
	}
	if token == "" {
		var err error
		token, err = NewToken()
		if err != nil {
			return Bundle{}, err
		}
	}

	mgmtMap := fmt.Sprintf("%d:9100", port)

	compose := fmt.Sprintf(`# hapanel node — generated for %q
# 8443 = клиенты (HAProxy TCP) | %d = панель → агент напрямую (HTTP)
# Конфиг фронтендов пишет агент в backends.d (без bind-mount файла haproxy.cfg).
services:
  haproxy:
    image: haproxy:3.0-alpine
    container_name: haproxy
    restart: unless-stopped
    ports:
      - "8443:8443"
    volumes:
      - ./haproxy/backends.d:/etc/haproxy/backends.d
      - ./certs:/etc/haproxy/certs:ro
    command: ["haproxy", "-W", "-db", "-f", "/etc/haproxy/backends.d"]
    expose:
      - "9999"
    sysctls:
      net.ipv4.ip_local_port_range: "1024 65535"
      net.ipv4.tcp_tw_reuse: "1"
      net.ipv4.tcp_fin_timeout: "15"
    ulimits:
      nofile:
        soft: 200000
        hard: 200000
    depends_on:
      - agent
    networks:
      - hapnet

  agent:
    image: %s
    container_name: hapanel-agent
    restart: unless-stopped
    ports:
      - %q
    environment:
      HAPANEL_TOKEN: %q
      HAPANEL_LISTEN: "0.0.0.0:9100"
      HAPROXY_SOCKET: tcp://haproxy:9999
      HAPROXY_BACKENDS_DIR: /etc/haproxy/backends.d
      HAPANEL_STATE_PATH: /var/lib/hapanel/state.json
      DOCKER_HOST: unix:///var/run/docker.sock
      HAPROXY_CONTAINER: haproxy
      HAPANEL_DEFAULT_BACKEND: app
      HOST_PROC: /host/proc
      HOST_ROOT: /host/root
    volumes:
      - ./haproxy/backends.d:/etc/haproxy/backends.d
      - agent-state:/var/lib/hapanel
      - /var/run/docker.sock:/var/run/docker.sock
      - /proc:/host/proc:ro
      - /:/host/root:ro
    networks:
      - hapnet
    user: "0:0"
    group_add:
      - "${DOCKER_GID:-0}"

networks:
  hapnet:
    driver: bridge

volumes:
  agent-state:
`, name, port, AgentImage, mgmtMap, token)

	baseCfg := `# Managed by hapanel agent — do not edit by hand
# Client frontends live here (not a host bind-mounted haproxy.cfg).
global
    maxconn 50000
    nbthread 2
    hard-stop-after 5m
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
    master-worker

defaults
    mode    tcp
    no log
    option  splice-auto
    timeout connect 5s
    timeout client  30m
    timeout server  30m
    timeout tunnel  30m
    timeout client-fin 30s
    timeout server-fin 30s
    retries 2

frontend https_front
    mode tcp
    bind *:8443
    maxconn 40000
    default_backend app
`

	appCfg := `# Managed by hapanel agent — do not edit by hand
backend app
    mode tcp
    balance leastconn
`

	envFile := fmt.Sprintf(`HAPANEL_TOKEN=%s
# Linux: getent group docker | cut -d: -f3
DOCKER_GID=0
`, token)

	readme := fmt.Sprintf(`# hapanel node: %s

Порты:
- **8443** — клиентский трафик (HAProxy TCP passthrough → app)
- **%d** — панель ↔ агент напрямую (HTTP /_hapctl)

Важно: ограничьте доступ к порту %d (firewall: только IP панели).

1. Установите Docker + Compose на VPS.
2. Скопируйте файлы бандла в /opt/hapanel-node (достаточно docker-compose.yml + backends.d).
3. mkdir -p certs haproxy/backends.d
4. docker pull %s
5. DOCKER_GID=$(getent group docker | cut -d: -f3) docker compose up -d
6. В панели → «Проверить связь».

Агент сам пишет фронтенды :8443 в backends.d при старте.
`, name, port, port, AgentImage)

	nodeURL := BuildURL(host, port)
	commands := fmt.Sprintf(`mkdir -p /opt/hapanel-node/haproxy/backends.d /opt/hapanel-node/certs
cd /opt/hapanel-node
# запишите docker-compose.yml и файлы backends.d из бандла панели
# 8443 = клиенты TCP, %d = панель→агент (HTTP); порт 80 не публикуем
export DOCKER_GID=$(getent group docker | cut -d: -f3)
docker compose up -d
# затем «Проверить связь» в панели → %s
`, port, nodeURL)

	return Bundle{
		Token:      token,
		URL:        nodeURL,
		Host:       host,
		Port:       port,
		AgentImage: AgentImage,
		Files: map[string]string{
			"docker-compose.yml":                      compose,
			"haproxy/backends.d/00-hapanel-base.cfg":  baseCfg,
			"haproxy/backends.d/app.cfg":              appCfg,
			".env":                                    envFile,
			"README.md":                               readme,
		},
		Commands: commands,
	}, nil
}

// GenerateFromURL regenerates a bundle for an existing node.
// Management is always HTTP to the agent (never via HAProxy).
func GenerateFromURL(name, rawURL, token string) (Bundle, error) {
	host, port, err := ParseURL(rawURL)
	if err != nil {
		return Bundle{}, err
	}
	return Generate(name, host, port, token)
}
