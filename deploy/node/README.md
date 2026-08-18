# Hapanel node stack (HAProxy + agent)

## Ports

| Port | Role |
|------|------|
| **8443** | Клиенты (HAProxy TCP → app). Без `/_hapctl`. |
| **47893** | Панель ↔ агент напрямую (`47893:9100`, HTTP `/_hapctl`) |

## Quick start

Образы по умолчанию: `azamatbash/hanode:0.1.0` + `haproxy:3.0-alpine`
(или бандл из мастера панели).

```bash
cd deploy/node
cp .env.example .env
# edit HAPANEL_TOKEN

./scripts/gen-dev-certs.sh

docker compose pull
docker compose up -d
# local agent: uncomment build in docker-compose.yml, then --build
```
Management API (plain HTTP on **:47893**, agent published directly):

```bash
curl -H "Authorization: Bearer $HAPANEL_TOKEN" \
  http://127.0.0.1:47893/_hapctl/v1/health
```

`/_hapctl` is **not** available on :443 (clients only). Firewall: allow **47893** only from the panel IP.

## Services

| Service | Role |
|---------|------|
| `haproxy` | TCP :8443 clients |
| `agent` | Management API published as host **:47893** → container `:9100` |

## Volumes

- `./certs/site.pem` — combined fullchain+key for HAProxy (clients)
- `./haproxy/backends.d` — agent-managed server snippets
- `haproxy-run` — shared Unix admin socket
- `agent-state` — JSON persistence of servers

## HAProxy version

Official `haproxy:3.0-alpine` with configs mounted from `./haproxy/`. Soft reload uses `SIGUSR2` (master-worker `-W`). Optional local bake: `./haproxy/Dockerfile`.
