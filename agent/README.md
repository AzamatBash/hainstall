# Hapanel Node Agent

Go agent that runs beside HAProxy on each VPS and exposes a management HTTP API
under `/_hapctl/v1`.

## Build

```bash
cd agent
go build -ldflags "-X github.com/azabash/hapanel/agent/internal/api.Version=0.1.0" -o bin/hapanel-agent ./cmd/agent
```

Or via Docker (see `deploy/node`).

## Run (local / bare metal)

```bash
export HAPANEL_TOKEN=change-me
export HAPROXY_SOCKET=/var/run/haproxy/admin.sock
export HAPROXY_BACKENDS_DIR=/etc/haproxy/backends.d
export HAPANEL_STATE_PATH=/var/lib/hapanel/state.json
export DOCKER_HOST=unix:///var/run/docker.sock
export HAPROXY_CONTAINER=haproxy

./bin/hapanel-agent
```

Listens on `0.0.0.0:9100` by default (`HAPANEL_LISTEN`).

## Auth

All endpoints except `GET /_hapctl/v1/health` require:

```
Authorization: Bearer <HAPANEL_TOKEN>
```

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/_hapctl/v1/health` | Liveness / version |
| GET | `/_hapctl/v1/stats` | Active sessions + FE/BE summary |
| GET | `/_hapctl/v1/backends` | List backends and servers |
| POST | `/_hapctl/v1/backends` | Add/update server (persist + reload) |
| DELETE | `/_hapctl/v1/backends/{backend}/{name}` | Remove server (persist + reload) |
| POST | `/_hapctl/v1/haproxy/reload` | Rewrite cfg + SIGUSR2/SIGHUP |
| POST | `/_hapctl/v1/haproxy/restart` | Docker restart of HAProxy container |

### POST /backends body

```json
{
  "backend": "app",
  "name": "srv1",
  "address": "1.2.3.4",
  "port": 443,
  "weight": 100
}
```

`backend` defaults to `app` if omitted.

## Layout

```
cmd/agent          entrypoint
internal/api       HTTP handlers (chi)
internal/auth      bearer token middleware
internal/haproxy   runtime socket + config writer
internal/dockerctl Docker Engine reload/restart
internal/store     JSON state persistence
```

## HAProxy notes

- Targets HAProxy **2.8+ / 3.x** with a Unix admin socket and master-worker (`-W`).
- Server inventory is written to `/etc/haproxy/backends.d/<backend>.cfg` then the
  HAProxy container is signaled (`SIGUSR2`, fallback `SIGHUP`).
- Runtime `add server` / `del server` are attempted as a best-effort fast path.
