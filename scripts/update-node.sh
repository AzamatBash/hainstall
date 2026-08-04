#!/usr/bin/env bash
# Обновление уже установленной ноды hapanel (TCP runtime API вместо unix sock).
# Сохраняет .env (токен) и haproxy/backends.d.
#
# Одной командой на VPS:
#   curl -fsSL https://raw.githubusercontent.com/AzamatBash/hainstall/main/scripts/update-node.sh | bash
#
set -euo pipefail

AGENT_IMAGE="${AGENT_IMAGE:-azamatbash/hanode:0.1.0}"
INSTALL_DIR="${HAPANEL_NODE_DIR:-${1:-/opt/hapanel-node}}"
MGMT_PORT="${HAPANEL_MGMT_PORT:-}"

die() { echo "Ошибка: $*" >&2; exit 1; }
need_cmd() { command -v "$1" >/dev/null 2>&1 || die "нужна команда «$1»"; }

need_cmd docker
need_cmd awk
if ! docker compose version >/dev/null 2>&1; then
  die "нужен Docker Compose plugin (docker compose version)"
fi

[[ -d "$INSTALL_DIR" ]] || die "каталог $INSTALL_DIR не найден"
cd "$INSTALL_DIR"

if [[ ! -f .env ]]; then
  die "нет $INSTALL_DIR/.env — без HAPANEL_TOKEN нельзя продолжить"
fi
# shellcheck disable=SC1091
set -a
# shellcheck source=/dev/null
source ./.env
set +a
[[ -n "${HAPANEL_TOKEN:-}" ]] || die "в .env нет HAPANEL_TOKEN"

# Порт панели→агент: из env, иначе из текущего compose, иначе 47893
if [[ -z "$MGMT_PORT" && -f docker-compose.yml ]]; then
  MGMT_PORT="$(awk -F'"' '/"[0-9]+:9100"/ {print $2; exit}' docker-compose.yml | cut -d: -f1 || true)"
fi
MGMT_PORT="${MGMT_PORT:-47893}"

ts="$(date +%Y%m%d-%H%M%S)"
mkdir -p backups "$INSTALL_DIR/haproxy/backends.d" "$INSTALL_DIR/certs"
[[ -f docker-compose.yml ]] && cp -a docker-compose.yml "backups/docker-compose.yml.$ts"
[[ -f haproxy/haproxy.cfg ]] && cp -a haproxy/haproxy.cfg "backups/haproxy.cfg.$ts"

echo "==> Пишу docker-compose.yml + haproxy.cfg (mgmt :$MGMT_PORT, image $AGENT_IMAGE)"

cat > docker-compose.yml <<EOF
# hapanel node — updated by update-node.sh
# 80/443 = клиенты | ${MGMT_PORT} = панель→агент (HTTP)
services:
  haproxy:
    image: haproxy:3.0-alpine
    container_name: haproxy
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./haproxy/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro
      - ./haproxy/backends.d:/etc/haproxy/backends.d
      - ./certs:/etc/haproxy/certs:ro
    command: ["haproxy", "-W", "-db", "-f", "/usr/local/etc/haproxy/haproxy.cfg", "-f", "/etc/haproxy/backends.d"]
    expose:
      - "9999"
    depends_on:
      - agent
    networks:
      - hapnet

  agent:
    image: ${AGENT_IMAGE}
    container_name: hapanel-agent
    restart: unless-stopped
    ports:
      - "${MGMT_PORT}:9100"
    environment:
      HAPANEL_TOKEN: \${HAPANEL_TOKEN:?set HAPANEL_TOKEN in .env}
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
      - "\${DOCKER_GID:-0}"

networks:
  hapnet:
    driver: bridge

volumes:
  agent-state:
EOF

cat > haproxy/haproxy.cfg <<'EOF'
# hapanel node HAProxy — клиенты :80/:443; runtime API TCP :9999 (docker network)
global
    maxconn 20000
    stats socket ipv4@0.0.0.0:9999 level admin
    stats timeout 30s
    master-worker

defaults
    mode    tcp
    no log
    timeout connect 10s
    timeout client  30m
    timeout server  30m
    timeout tunnel  30m
    retries 3

frontend http_plain
    mode http
    bind *:80
    acl is_acme path_beg /.well-known/acme-challenge/
    http-request redirect scheme https code 301 unless is_acme
    use_backend acme if is_acme

frontend https_front
    mode tcp
    bind *:443
    tcp-request inspect-delay 5s
    tcp-request content accept if { req_ssl_hello_type 1 }
    default_backend app

backend acme
    mode http
    server local 127.0.0.1:8080
EOF

if [[ ! -f haproxy/backends.d/app.cfg ]]; then
  cat > haproxy/backends.d/app.cfg <<'EOF'
# Managed by hapanel agent — do not edit by hand
backend app
    mode tcp
    balance leastconn
    option ssl-hello-chk
EOF
fi

# Убрать старый shared volume haproxy-run (unix sock), если был
export DOCKER_GID
DOCKER_GID="$(getent group docker 2>/dev/null | cut -d: -f3 || echo 0)"

echo "==> docker compose pull + up"
docker compose pull
docker compose up -d --remove-orphans
# старый volume с сокетом больше не нужен
docker volume rm hapanel-node_haproxy-run 2>/dev/null || true

echo "==> Проверка runtime API"
sleep 2
if docker compose exec -T agent wget -qO- --timeout=3 http://127.0.0.1:9100/_hapctl/v1/health >/dev/null 2>&1 \
  || docker compose exec -T agent /bin/true >/dev/null 2>&1; then
  :
fi

# stats через агент с токеном (если curl есть на хосте)
if command -v curl >/dev/null 2>&1; then
  code="$(curl -sS -o /tmp/hap-stats.json -w '%{http_code}' \
    -H "Authorization: Bearer ${HAPANEL_TOKEN}" \
    "http://127.0.0.1:${MGMT_PORT}/_hapctl/v1/stats" || echo 000)"
  echo "GET /_hapctl/v1/stats → HTTP $code"
  if [[ "$code" != "200" ]]; then
    echo "--- agent logs ---"
    docker compose logs --tail=40 agent || true
    echo "--- haproxy logs ---"
    docker compose logs --tail=20 haproxy || true
    die "stats ещё не 200 — смотрите логи выше"
  fi
fi

echo
echo "Готово. Нода: $INSTALL_DIR"
echo "В панели нажмите «Проверить связь»."
echo "Бэкапы: $INSTALL_DIR/backups/"
