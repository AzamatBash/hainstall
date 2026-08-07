#!/usr/bin/env bash
# Remnanode + Let's Encrypt + UFW (skill: vps-remnanode-secure)
# Usage: bash remnanode-secure-setup.sh DOMAIN SECRET_KEY [NODE_PORT] [PANEL_IP]
set -euo pipefail

DOMAIN="${1:?domain required}"
SECRET_KEY="${2:?secret key required}"
NODE_PORT="${3:-2222}"
PANEL_IP="${4:-}"

export DEBIAN_FRONTEND=noninteractive

echo "==> Domain=${DOMAIN} NODE_PORT=${NODE_PORT} PANEL_IP=${PANEL_IP:-anywhere}"

echo "==> Packages"
apt-get update -qq
apt-get install -y -qq ca-certificates curl gnupg lsb-release ufw nginx certbot >/tmp/apt-remna.log 2>&1 || {
  tail -40 /tmp/apt-remna.log
  exit 1
}

if ! command -v docker >/dev/null 2>&1; then
  echo "==> Install Docker"
  curl -fsSL https://get.docker.com | sh
fi
systemctl enable --now docker

# Stop common conflicts on 80/443
systemctl disable --now x-ui 2>/dev/null || true
systemctl disable --now xray 2>/dev/null || true

echo "==> UFW baseline"
ufw --force reset >/dev/null 2>&1 || true
ufw default deny incoming
ufw default allow outgoing
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
if [[ -n "$PANEL_IP" ]]; then
  ufw allow from "$PANEL_IP" to any port "$NODE_PORT" proto tcp comment 'remnanode mgmt from panel'
else
  ufw allow "${NODE_PORT}/tcp" comment 'remnanode mgmt'
fi
ufw --force enable
ufw status | head -30

echo "==> ACME nginx stub"
mkdir -p /var/www/certbot
cat >/etc/nginx/sites-available/acme-stub <<EOF
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name ${DOMAIN} _;

    location /.well-known/acme-challenge/ {
        root /var/www/certbot;
    }

    location / {
        return 404;
    }
}
EOF
ln -sfn /etc/nginx/sites-available/acme-stub /etc/nginx/sites-enabled/acme-stub
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl enable --now nginx
systemctl reload nginx

echo "==> Let's Encrypt"
if [[ ! -d "/etc/letsencrypt/live/${DOMAIN}" ]]; then
  # Prefer webroot (nginx already on :80)
  certbot certonly --webroot -w /var/www/certbot \
    -d "${DOMAIN}" \
    --agree-tos --non-interactive --register-unsafely-without-email \
    || certbot certonly --standalone -d "${DOMAIN}" \
         --agree-tos --non-interactive --register-unsafely-without-email \
         --preferred-challenges http
fi
ls -la "/etc/letsencrypt/live/${DOMAIN}/"

mkdir -p /etc/letsencrypt/renewal-hooks/deploy
cat >/etc/letsencrypt/renewal-hooks/deploy/reload-remnanode-ssl.sh <<'HOOK'
#!/bin/bash
systemctl reload nginx 2>/dev/null || true
if [[ -d /opt/remnanode ]]; then
  cd /opt/remnanode && docker compose restart remnanode || docker restart remnanode || true
fi
HOOK
chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-remnanode-ssl.sh
systemctl enable --now certbot.timer 2>/dev/null || true

echo "==> Write /opt/remnanode"
mkdir -p /opt/remnanode
# Escape for YAML double-quoted string
SECRET_ESC="${SECRET_KEY//\\/\\\\}"
SECRET_ESC="${SECRET_ESC//\"/\\\"}"

cat >/opt/remnanode/docker-compose.yml <<EOF
services:
  remnanode:
    container_name: remnanode
    hostname: remnanode
    image: remnawave/node:latest
    network_mode: host
    restart: always
    cap_add:
      - NET_ADMIN
    ulimits:
      nofile:
        soft: 1048576
        hard: 1048576
    volumes:
      - /etc/letsencrypt:/etc/letsencrypt:ro
    environment:
      - NODE_PORT=${NODE_PORT}
      - SECRET_KEY="${SECRET_ESC}"
EOF

cd /opt/remnanode
docker compose pull
docker compose up -d
sleep 3
docker ps --filter name=remnanode --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}'
ss -tlnp | grep -E ":${NODE_PORT}\\b|:80\\b|:443\\b" || true

echo
echo "=== DONE ==="
echo "DOMAIN: ${DOMAIN}"
echo "NODE_PORT: ${NODE_PORT}"
echo "Xray certificateFile: /etc/letsencrypt/live/${DOMAIN}/fullchain.pem"
echo "Xray keyFile:         /etc/letsencrypt/live/${DOMAIN}/privkey.pem"
echo "certbot.timer: $(systemctl is-active certbot.timer 2>/dev/null || echo n/a)"
