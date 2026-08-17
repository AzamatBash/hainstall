# hapanel

Central control panel for managing multiple HAProxy nodes through their agents.
Inspired by Remnawave-style ops panels: one UI, many nodes, agent API under `/_hapctl/v1`.

## Architecture

```
┌─────────────┐   HTTP :47893 (agent напрямую)         ┌──────────────────┐
│  Panel UI   │ ─────────────────────────────────────► │  Node agent:9100 │
│  + Go API   │   Authorization: Bearer <token>        │  published 47893 │
│  :3080      │                                        └──────────────────┘
└─────────────┘                                                 │
      │                                              docker sock │
      ▼                                                          ▼
   SQLite (nodes)                                      ┌──────────────────┐
                                                       │  HAProxy :443/80 │
                                                       │  clients only    │
                                                       └──────────────────┘
```

| Component | Path | Role |
|-----------|------|------|
| Panel server | `panel/server` | Go API, SQLite, HTTP(+uTLS for https) client to agents, serves UI |
| Panel web | `panel/web` | Vite + React + TypeScript dashboard |
| Panel deploy | `deploy/panel` | Docker multi-stage image + compose |
| Node agent | `agent/` | Per-node HAP control API |
| Node deploy | `deploy/node/` | HAProxy 3.0 (clients) + agent (**47893** management HTTP) |

### Ports on each node

| Port | Traffic |
|------|---------|
| **80 / 443** | Client traffic only (HAProxy → app backends). No `/_hapctl` on 443. |
| **47893** | Panel ↔ agent directly (`http://host:47893/_hapctl/v1/...`). Not through HAProxy. |

Agent listens on `:9100` inside the container and is published as host `47893:9100`. Restrict 47893 to the panel IP via firewall.

### Panel → node transport

- Default provision URL is **`http://host:47893`** (plain HTTP, no uTLS).
- Legacy `https://` URLs still use **[uTLS](https://github.com/refraction-networking/utls)** (`HAPANEL_UTLS_HELLO`, default `HelloRandomized`).
- For self-signed HTTPS MVP certs, set `HAPANEL_INSECURE_SKIP_VERIFY=true` (default in Docker).

### Agent API (must match)

All agent routes are under `/_hapctl/v1/...` with `Authorization: Bearer <token>`:

- `GET /_hapctl/v1/health`
- `GET /_hapctl/v1/stats`
- `GET /_hapctl/v1/backends`
- `POST /_hapctl/v1/backends` — `{backend,name,address,port,weight}`
- `DELETE /_hapctl/v1/backends/{backend}/{name}`
- `POST /_hapctl/v1/haproxy/reload`
- `POST /_hapctl/v1/haproxy/restart`

## Docker Hub images

| Image | Tags | Role |
|-------|------|------|
| `azamatbash/hapanel` | `0.1.0`, `latest` | Panel (UI + API). **Prod digest:** `sha256:0ff113b2f6d8091a67e57513b627c43747f6d912cb5c334ec7b684bc12ff9591` (см. `deploy/panel/PROD_IMAGE.txt`) |
| `azamatbash/hanode` | `0.1.0`, `latest` | Node agent |
| `haproxy:3.0-alpine` | official | HAProxy on nodes |

Теги `0.1.0` и `latest` указывают на **один и тот же** образ, что крутится на проде (одни и те же layers). Ставить лучше по digest:

```bash
docker pull azamatbash/hapanel@sha256:0ff113b2f6d8091a67e57513b627c43747f6d912cb5c334ec7b684bc12ff9591
```

В репозитории точные бинарник и UI с прода лежат в `deploy/panel/prod-frozen/` (сверка с живым контейнером).

### Push (maintainer)

```bash
docker login
./scripts/push-images.sh
# optional: VERSION=0.1.0 ./scripts/push-images.sh
```

Builds from repo root:
- panel → `deploy/panel/Dockerfile`
- agent → `deploy/node/agent/Dockerfile`

### Как развернуть панель (Docker Hub)

Нужен сервер с Docker и плагином Compose. Образ панели: `azamatbash/hapanel:0.1.0`.

**Вариант 1 — одной командой (рекомендуется)**

Скрипт сам создаст `/opt/hapanel`, сгенерирует пароль и JWT-секрет, скачает образ и поднимет контейнер. В конце выведет URL и пароль.

```bash
curl -fsSL https://raw.githubusercontent.com/AzamatBash/hainstall/main/scripts/install-panel.sh | bash
```

Или из клона репозитория:

```bash
bash scripts/install-panel.sh
# другой каталог: HAPANEL_DIR=/opt/hapanel bash scripts/install-panel.sh
```

Пароль также лежит в `/opt/hapanel/.env` (права `600`). Сохраните его — скрипт повторно не показывает. Защита входа: **5 неверных попыток с одного IP → блокировка на 15 минут** (429).

**Вариант 2 — вручную**

```bash
mkdir -p /opt/hapanel && cd /opt/hapanel
# скопируйте deploy/panel/docker-compose.yml и .env.example из репо
cp .env.example .env
# задайте PANEL_PASSWORD и PANEL_JWT_SECRET
# опционально: PANEL_BASE_PATH=/секретный_путь  PANEL_BIND=127.0.0.1
docker compose pull
docker compose up -d
```

Открыть: `http://SERVER:3080` (или с `PANEL_BASE_PATH`, если задан). Данные SQLite — в volume `panel-data`.

Обновление панели до актуального образа с Hub:

```bash
cd /opt/hapanel
docker compose pull
docker compose up -d
```

Локальная пересборка того же тега из исходников: `docker compose up -d --build`.

### Nodes from the panel wizard

В мастере «Добавить ноду» панель сама генерирует `docker-compose.yml` с образом
`azamatbash/hanode:0.1.0` (без локальной сборки), HAProxy — `haproxy:3.0-alpine`.
На VPS: скопировать файлы бандла → `docker pull azamatbash/hanode:0.1.0` →
`docker compose up -d` → «Проверить связь».

## Quickstart — panel (Docker)

```bash
bash scripts/install-panel.sh
# → URL + пароль в конце вывода; данные в /opt/hapanel
```

Или локально из `deploy/panel`:

```bash
cd deploy/panel
cp .env.example .env
# edit PANEL_PASSWORD and PANEL_JWT_SECRET
docker compose pull          # from Hub
docker compose up -d
# or local build: docker compose up -d --build
```

Open http://localhost:3080 — sign in with `PANEL_PASSWORD` (из вывода install
или из `.env`).

SQLite data lives in the `panel-data` volume (`/app/data/panel.db`).

## Quickstart — panel (local)

Requirements: Go 1.22+, Node 20+.

```bash
# terminal 1 — API
cd panel/server
export PANEL_PASSWORD=changeme
export PANEL_JWT_SECRET=dev-secret
export PANEL_DB_PATH=./data/panel.db
export PANEL_STATIC_DIR=../web/dist
export HAPANEL_INSECURE_SKIP_VERIFY=true
go run ./cmd/panel

# terminal 2 — UI (dev with API proxy), or build static into dist
cd panel/web
npm install
npm run dev          # http://localhost:5173 → proxies /api to :3080
# or: npm run build  # then panel serves ../web/dist on :3080
```

## Quickstart — node

Prefer the install bundle from the panel wizard (`azamatbash/hanode:0.1.0`).

Local stack (`deploy/node`):

```bash
cd deploy/node
cp .env.example .env
# set HAPANEL_TOKEN (and DOCKER_GID=`getent group docker | cut -d: -f3`)
./scripts/gen-dev-certs.sh
docker compose pull          # hanode + haproxy:3.0-alpine
docker compose up -d
# local agent rebuild: uncomment build in docker-compose.yml, then --build
```

Agent API is at `http://<node>:47893/_hapctl/v1/...` (agent published directly). Clients use `:443`. Then:

1. Open the panel → **Add node**
2. Enter name, host, management port (**47893** by default) — panel URL becomes `http://host:47893`
3. Use node detail to view stats, manage backends, reload/restart HAProxy

**Local Docker tip:** the panel container cannot reach the host via `127.0.0.1`. Either put both stacks on one Docker network and use `http://hapanel-agent:9100`, or publish 47893 and use `http://host.docker.internal:47893` with `extra_hosts: ["host.docker.internal:host-gateway"]`.

See `agent/README.md` and `deploy/node/README.md` for details.
## Environment

| Variable | Default | Description |
|----------|---------|-------------|
| `PANEL_LISTEN` | `:3080` | Listen address |
| `PANEL_PASSWORD` | `changeme` | Admin password |
| `PANEL_JWT_SECRET` | (dev value) | JWT / session signing key |
| `PANEL_DB_PATH` | `./data/panel.db` | SQLite path |
| `PANEL_STATIC_DIR` | `./web/dist` | Built UI directory |
| `HAPANEL_INSECURE_SKIP_VERIFY` | `true` (Docker) | Skip verify on panel→node TLS |
| `HAPANEL_UTLS_HELLO` | `randomized` | uTLS fingerprint: `randomized`, `chrome`, `firefox`, `golang` |

Do not commit real secrets (`.env`, tokens, passwords).
