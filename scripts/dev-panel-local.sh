#!/bin/bash
set -euo pipefail
export PATH="$HOME/.local/node/bin:$HOME/.local/go/bin:$PATH"
ROOT=/home/azabash/hapanel

# rebuild panel
cd "$ROOT/panel/server"
go build -o /tmp/hapanel-panel ./cmd/panel

pkill -f '/tmp/hapanel-panel' 2>/dev/null || true
pkill -f 'vite --host 127.0.0.1' 2>/dev/null || true
sleep 0.5

mkdir -p "$ROOT/panel/server/data"
cd "$ROOT/panel/server"
PANEL_PASSWORD='changeme' PANEL_JWT_SECRET='dev-secret-local' \
PANEL_DB_PATH=./data/panel.db PANEL_STATIC_DIR=../web/dist \
HAPANEL_INSECURE_SKIP_VERIFY=true \
nohup /tmp/hapanel-panel > /tmp/hapanel-panel.log 2>&1 &
echo "panel_pid=$!"

cd "$ROOT/panel/web"
npm install --no-fund --no-audit >/tmp/hapanel-npm-install.log 2>&1
nohup npm run dev -- --host 127.0.0.1 --port 5173 > /tmp/hapanel-vite.log 2>&1 &
echo "vite_pid=$!"
sleep 4
echo "=== panel ==="; tail -5 /tmp/hapanel-panel.log
echo "=== vite ==="; tail -15 /tmp/hapanel-vite.log
curl -sS -m 3 -o /dev/null -w "vite_http=%{http_code}\n" http://127.0.0.1:5173/ || true
curl -sS -m 3 -o /dev/null -w "panel_http=%{http_code}\n" http://127.0.0.1:3080/api/auth/me || true
echo "UI: http://127.0.0.1:5173/  password: changeme  then /tasks"
