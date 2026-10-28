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
| `azamatbash/hapanel` | `0.1.2`, `latest` | Panel (UI + API). **Prod digest:** `sha256:29ad6f65637e7086beeda0e582dc5b741ae6fa713736d3b25834e02afe11ba91` (см. `deploy/panel/PROD_IMAGE.txt`) |
| `azamatbash/hanode` | `0.1.2`, `latest` | Node agent (`sha256:4375dcdd09e0209681b90d54e10e2edcd59109648dc204ec6794e5a780a77fea`) |
| `haproxy:3.0-alpine` | official | HAProxy on nodes |

Теги `0.1.2` и `latest` указывают на **один и тот же** образ, что крутится на проде (одни и те же layers). Ставить лучше по digest:

```bash
docker pull azamatbash/hapanel@sha256:29ad6f65637e7086beeda0e582dc5b741ae6fa713736d3b25834e02afe11ba91
```

В репозитории точные бинарник и UI с прода лежат в `deploy/panel/prod-frozen/` (сверка с живым контейнером).

### Push (maintainer)

```bash
docker login
REBUILD_PANEL=1 ./scripts/push-images.sh
# optional: PANEL_VERSION=0.1.76 REBUILD_PANEL=1 ./scripts/push-images.sh
```

Builds from repo root:
- panel → `deploy/panel/Dockerfile`
- agent → `deploy/node/agent/Dockerfile`

`PANEL_VERSION` (default in the script) is both the panel tag and, with `REBUILD_PANEL=1`, the version stamped into the binary (`--build-arg PANEL_VERSION=...`, shown as `version` in `GET /api/v1/config`). Without `REBUILD_PANEL=1` the script only re-tags an image built elsewhere, which keeps the version it was built with. A panel build without `--build-arg PANEL_VERSION` reports `dev`:

```bash
docker build --build-arg PANEL_VERSION=0.1.76 -t azamatbash/hapanel:0.1.76 -f deploy/panel/Dockerfile .
```

### Как развернуть панель (Docker Hub)

Нужен сервер с Docker и плагином Compose. Образ панели: `azamatbash/hapanel:0.1.2`.

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

Локальная пересборка того же тега из исходников: `docker compose up -d --build` (раскомментируйте блок `build:` вместе с `args: PANEL_VERSION` в `docker-compose.yml` и задайте `PANEL_VERSION`, иначе `version` в `GET /api/v1/config` будет `dev`).

### Nodes from the panel wizard

В мастере «Добавить ноду» панель сама генерирует `docker-compose.yml` с образом
`azamatbash/hanode:0.1.2` (без локальной сборки), HAProxy — `haproxy:3.0-alpine`.
На VPS: скопировать файлы бандла → `docker pull azamatbash/hanode:0.1.2` →
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

Prefer the install bundle from the panel wizard (`azamatbash/hanode:0.1.2`).

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
| `PANEL_API_KEY` | (empty) | Optional env bootstrap key for `GET /api/v1/hanodes` (prefer keys from UI **api**) |
| `PANEL_DB_PATH` | `./data/panel.db` | SQLite path |
| `PANEL_STATIC_DIR` | `./web/dist` | Built UI directory |
| `HAPANEL_INSECURE_SKIP_VERIFY` | `true` (Docker) | Skip verify on panel→node TLS |
| `HAPANEL_UTLS_HELLO` | `randomized` | uTLS fingerprint: `randomized`, `chrome`, `firefox`, `golang` |

### База знаний (`/kb`)

Встроенные технические статьи из `panel/kb/` (источник vpn-hub): каталог по разделам, поиск, код с «Копировать», сборщик плейсхолдеров (домен, Reality-ключи и т.д.) в `localStorage` браузера. При сборке UI: `npm run kb` / `prebuild` читает `KB_SRC` (по умолчанию `panel/kb`).

### Export API (hanode stats for another panel)

In the panel UI open **api** → **Сгенерировать**. The plaintext key is shown once; afterwards only the prefix is listed. You can revoke/delete keys there.

Read-only snapshot of all hanodes (CPU, load, live traffic rates). Auth: panel-generated key, optional `PANEL_API_KEY`, or admin JWT.

```bash
curl -sS -H "X-API-Key: $HAPANEL_API_KEY" "https://panel.example/api/v1/hanodes"
# optional period totals (hourly buckets, max 90d):
curl -sS -H "Authorization: Bearer $HAPANEL_API_KEY" "https://panel.example/api/v1/hanodes?hours=24"
curl -sS -H "X-API-Key: $HAPANEL_API_KEY" "https://panel.example/api/v1/hanodes/<id>?hours=24"
```

Fields per node: `id`, `name`, `country`, `status`, `last_seen`, `cpu`, `load_avg`, `down_bps`, `up_bps`, `sessions`, `net_rx_bytes`, `net_tx_bytes`, `agent_version`, `updated_at`, and with `?hours=` also `period_rx_bytes` / `period_tx_bytes`. `net_rx_bytes`, `net_tx_bytes` and other empty optional fields are omitted when zero or unknown; `cpu`, `down_bps`, `up_bps` and `sessions` are sent even when 0. No node tokens or management URLs.

### Права ключей и выгрузка настроек (`GET /api/v1/config`)

У API-ключа может быть набор прав (`scopes`). Любой действующий ключ, как и раньше, читает `GET /api/v1/hanodes` и `/api/v1/hanodes/{id}`. Ключ с правом `config` дополнительно читает `GET /api/v1/config`. Ключи из интерфейса (**api** → **Сгенерировать**) и ключ из `PANEL_API_KEY` прав не имеют: на `/api/v1/config` они получают `403 {"error":"у ключа нет права config"}`. Сессия администратора (JWT) открывает и этот маршрут.

Как выпустить ключ с правом `config`:

- API (сессия администратора): `POST /api/settings/api-keys` с телом `{"name":"vpn-expert","scopes":["config"]}`. Без `scopes` создаётся прежний ключ, неизвестное право — `400`. `GET /api/settings/api-keys` показывает `scopes` у каждого ключа.
- CLI в том же бинарнике, без пароля, можно рядом с работающей панелью:

```bash
docker exec hapanel-panel /app/panel apikey create --name vpn-expert --scope config
docker exec hapanel-panel /app/panel apikey list
```

`create` печатает в stdout только сам ключ (одну строку), в stderr — id, префикс и права. `list` показывает id, имя, префикс, права, даты создания, последнего использования и отзыва; ключи и хеши не печатает. CLI открывает существующую базу из `PANEL_DB_PATH` (или `--db <путь>`), не создаёт её, не запускает HTTP-сервер, опросчики и миграции. Путь — обычный путь к файлу: значения с префиксом `file:` или с символами `?` и `#` команда отклоняет. Соединение получает те же `busy_timeout` (5 с), `synchronous` и `foreign_keys`, что и сервер, поэтому запись рядом с работающей панелью ждёт блокировку, а не падает с `database is locked`; режим журнала (WAL) не меняется. Если новая версия панели ещё не запускалась на этой базе (нет колонки `api_keys.scopes`), команда завершается с ошибкой. Запускайте от пользователя контейнера (`panel`), без `-u root`. Без аргументов бинарник, как и раньше, запускает сервер. Отзыв и удаление ключей — в интерфейсе (**api**).

Колонка `api_keys.scopes TEXT NOT NULL DEFAULT ''` добавляется при старте: схема только расширяется, образ 0.1.75 колонку игнорирует, откат возможен.

`GET /api/v1/config` — снимок настроек только из SQLite панели: без обращений к агентам нод и Remnawave, без записи в БД (кроме `last_used_at` ключа).

```bash
curl -sS -H "X-API-Key: $HAPANEL_CONFIG_KEY" "https://panel.example/<PANEL_BASE_PATH>/api/v1/config"
```

Ответ `200`, `Content-Type: application/json`, заголовок `ETag: "<snapshot_hash>"`:

```json
{
  "schema": "hapanel.config.v1",
  "version": "0.1.76",
  "generated_at": "2026-10-26T12:00:00Z",
  "snapshot_hash": "sha256:…",
  "sections": {
    "hanodes": {"status": "ok", "content_hash": "sha256:…", "items": []},
    "links": {"status": "ok", "content_hash": "sha256:…", "items": []},
    "remna_panels": {"status": "ok", "content_hash": "sha256:…", "items": []},
    "remna_nodes": {"status": "ok", "content_hash": "sha256:…", "items": []},
    "providers": {"status": "ok", "content_hash": "sha256:…", "items": []},
    "provider_accounts": {"status": "error", "items": []}
  }
}
```

- `version` — версия сборки: `--build-arg PANEL_VERSION=0.1.76` в `deploy/panel/Dockerfile` (`scripts/push-images.sh` с `REBUILD_PANEL=1` передаёт свой `PANEL_VERSION`), без аргумента `dev`. Время — RFC3339 UTC с точностью до секунды.
- Если запрос секции к БД не удался, у неё `"status":"error"`, `"items":[]` и нет `content_hash`, остальные секции отдаются. `links` получает `error` и при сбое `hanodes` или `remna_nodes`: без них не посчитать `active` и `match`.
- `content_hash` — SHA-256 канонического JSON массива `items`, `snapshot_hash` — канонического JSON объекта `sections` (вместе со `status` и `content_hash` секций). Канонический JSON в UTF-8: ключи объектов отсортированы на всех уровнях, без пробелов, целые числа как есть; в строках экранируются только `"` и `\`, управляющие символы (`\b`, `\f`, `\n`, `\r`, `\t`, остальные до U+001F — `\u00xx` строчными), а также U+2028 и U+2029 — как `\u2028` и `\u2029` (так всегда пишет кодировщик Go); HTML (`<`, `>`, `&`) и прочий текст не экранируются. В Python побайтно то же самое:

  ```python
  import hashlib, json

  def canonical(v):
      s = json.dumps(v, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
      return s.replace("\u2028", "\\u2028").replace("\u2029", "\\u2029").encode("utf-8")

  body = json.loads(response_bytes)
  for sec in body["sections"].values():
      if sec["status"] == "ok":
          assert sec["content_hash"] == "sha256:" + hashlib.sha256(canonical(sec["items"])).hexdigest()
  assert body["snapshot_hash"] == "sha256:" + hashlib.sha256(canonical(body["sections"])).hexdigest()
  ```

  Без `.replace(...)` хеш не совпадёт, если в тексте (имена, `notes`, `alert_note`) есть U+2028/U+2029. `generated_at` в хеш не входит. `backends_observed_at` обновляется с каждым опросом агента (раз в 5 с), поэтому хеш `hanodes` и `snapshot_hash` меняются, пока ноды онлайн.

Поля элементов:

| Секция | Сортировка | Поля |
|---|---|---|
| `hanodes` | `id` | `id`, `name`, `country`, `host` (только хост или IP из адреса управления; `""`, если адрес не разобрать), `sort_order`, `created_at`, `listen_ports`, `entrances` (`[{port, backend}]`), `protect` — действующий профиль, как в панели (`client_hello`, `client_hello_delay_sec`, `rate_limit`, `max_conn_per_ip`, `max_rate_per_ip`, `rate_window_sec`, `ru_only`, `synproxy`), `protect_default` (профиль не сохранялся, действует профиль по умолчанию), `remna_panel_id`, `provider_id`, `provider_account_id` (строка или `null`), `traffic_log`, `has_token`, `backends` (`[{backend, name, address, port}]` из последнего снимка агента, без служебных, по `backend`, `name`), `backends_observed_at` (время снимка или `null`) |
| `links` | `node_id`, `backend`, `server_name` | `node_id`, `backend`, `server_name`, `remna_panel_id`, `remna_address`, `remna_uuid` (нода каталога той же панели с тем же адресом без учёта регистра и пробелов по краям — тот же ключ `remna.AddressKey`, что у панели; иначе `null`), `match` (`unique` — одна такая нода, `none` — ни одной, `ambiguous` — несколько), `active` — то же правило, что у панели при подсчёте онлайна: `true`, если пара `backend` + `server_name` точно совпадает с `backend` + `name` одного из бэкендов в сохранённом снимке ноды, включая служебные; если в сохранённом снимке нет ни одного бэкенда (снимка нет или список пуст), `true` у всех связей ноды; у связи удалённой ноды всегда `false`. Пересчитать `active` по `hanodes.backends` нельзя: там служебные бэкенды скрыты, и снимок только со служебными бэкендами выглядит как `[]`, хотя связи с другими серверами неактивны |
| `remna_panels` | `id` | `id`, `name`, `has_api_key`, `created_at` |
| `remna_nodes` | `panel_id`, `uuid` | `panel_id`, `uuid`, `name`, `address`, `config_profile_uuid`, `inbound_uuids`, `inbound_tags`, `protocol` (переопределённый или вычисленный), `protocol_derived`, `protocol_override`, `roles` (`rn_front`, `rn_back`, `hp_front`, `hp_back`, `cdn_back`), `enabled_in_analytics`, `notes`, `alert_note` |
| `providers` | `id` | `id`, `name`, `created_at` |
| `provider_accounts` | `id` | `id`, `provider_id`, `created_at` |

Никогда не выгружаются: токены агентов и полный адрес управления нод (схема, порт, путь); статус, `last_seen`, CPU и трафик нод (они в `/api/v1/hanodes`); `base_url` и API-ключи панелей Remnawave; `login` аккаунтов; `login_url` и `favicon_url` провайдеров; `users_online`, `node_ok`, `last_seen_at`, `updated_at` каталога; задачи, команда, olcRTC, API-ключи и история метрик. Пример ответа на тестовых данных: `panel/server/internal/api/testdata/config_v1_example.json`.

Do not commit real secrets (`.env`, tokens, passwords).
