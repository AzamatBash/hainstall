#!/usr/bin/env bash
# Установка hapanel с Docker Hub (клон репозитория не нужен).
# Требуется: docker + plugin compose, openssl, curl.
#
# Одной командой:
#   curl -fsSL https://raw.githubusercontent.com/AzamatBash/hainstall/main/scripts/install-panel.sh | bash
#
set -euo pipefail

PANEL_IMAGE="${PANEL_IMAGE:-azamatbash/hapanel@sha256:97921b17ecb9c0854377b167dd323457b76379b97778cd2f9c59bdc92cb13223}"
PANEL_PORT="${PANEL_PORT:-3080}"
INSTALL_DIR="${HAPANEL_DIR:-${1:-/opt/hapanel}}"

die() { echo "Ошибка: $*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "нужна команда «$1»"
}

need_cmd docker
need_cmd openssl
need_cmd curl

if ! docker compose version >/dev/null 2>&1; then
  die "нужен Docker Compose plugin (docker compose version)"
fi

# Сильный пароль ≥24 символа: upper/lower/digits/symbols (base64 + замена +/).
gen_password() {
  local raw
  raw="$(openssl rand -base64 32 | tr -d '\n' | tr '+/' '@#')"
  echo "${raw:0:32}"
}

PANEL_PASSWORD="$(gen_password)"
PANEL_JWT_SECRET="$(openssl rand -hex 32)"

if [[ ${#PANEL_PASSWORD} -lt 24 ]]; then
  die "не удалось сгенерировать пароль достаточной длины"
fi

mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

umask 077
cat > .env <<EOF
# Сгенерировано scripts/install-panel.sh — храните в секрете (chmod 600)
PANEL_PASSWORD=${PANEL_PASSWORD}
PANEL_JWT_SECRET=${PANEL_JWT_SECRET}
PANEL_PORT=${PANEL_PORT}
HAPANEL_INSECURE_SKIP_VERIFY=true
HAPANEL_UTLS_HELLO=randomized
EOF
chmod 600 .env

cat > docker-compose.yml <<EOF
services:
  panel:
    image: ${PANEL_IMAGE}
    container_name: hapanel-panel
    ports:
      - "\${PANEL_PORT:-3080}:3080"
    environment:
      PANEL_PASSWORD: \${PANEL_PASSWORD}
      PANEL_JWT_SECRET: \${PANEL_JWT_SECRET}
      PANEL_LISTEN: ":3080"
      PANEL_DB_PATH: /app/data/panel.db
      PANEL_STATIC_DIR: /app/web/dist
      HAPANEL_INSECURE_SKIP_VERIFY: \${HAPANEL_INSECURE_SKIP_VERIFY:-true}
      HAPANEL_UTLS_HELLO: \${HAPANEL_UTLS_HELLO:-randomized}
      PANEL_BASE_PATH: \${PANEL_BASE_PATH:-}
      GEMINI_API_KEY: \${GEMINI_API_KEY:-}
      GROQ_API_KEY: \${GROQ_API_KEY:-}
      LLM_PROVIDER: \${LLM_PROVIDER:-gemini}
      PANEL_IP: \${PANEL_IP:-}
      LLM_HTTP_PROXY: \${LLM_HTTP_PROXY:-}
    volumes:
      - panel-data:/app/data
    restart: unless-stopped

volumes:
  panel-data:
EOF

echo "==> Каталог установки: $INSTALL_DIR"
echo "==> Образ: $PANEL_IMAGE"
echo "==> docker compose pull && up -d …"
docker compose pull
docker compose up -d

echo "==> Ожидание HTTP на порту ${PANEL_PORT} …"
ok=0
for _ in $(seq 1 60); do
  if curl -fsS -o /dev/null "http://127.0.0.1:${PANEL_PORT}/login" 2>/dev/null \
    || curl -fsS -o /dev/null "http://127.0.0.1:${PANEL_PORT}/" 2>/dev/null; then
    ok=1
    break
  fi
  sleep 1
done
if [[ "$ok" -ne 1 ]]; then
  die "панель не ответила на http://127.0.0.1:${PANEL_PORT} за 60 с — проверьте: docker compose -f ${INSTALL_DIR}/docker-compose.yml logs"
fi

detect_host() {
  local ip=""
  if command -v hostname >/dev/null 2>&1; then
    ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  if [[ -z "$ip" ]] || [[ "$ip" == "127.0.0.1" ]]; then
    ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}' || true)"
  fi
  if [[ -z "$ip" ]]; then
    ip="localhost"
  fi
  echo "$ip"
}

HOST="$(detect_host)"

cat <<EOF

════════════════════════════════════════════════════════
  hapanel установлен

  URL:      http://${HOST}:${PANEL_PORT}
  Пароль:   ${PANEL_PASSWORD}

  Пароль также записан в ${INSTALL_DIR}/.env (права 600).
  Сохраните пароль в надёжном месте — повторно он на экран
  не выводится.

  Защита входа: после 5 неверных попыток с одного IP —
  блокировка на 15 минут.
════════════════════════════════════════════════════════

EOF
