#!/usr/bin/env bash
# Push panel/node images to Docker Hub.
# Panel: by default re-pushes the exact production image (see deploy/panel/PROD_IMAGE.txt).
# Set REBUILD_PANEL=1 to build from deploy/panel/Dockerfile (dev only — digest will differ).
# Requires: docker login (run yourself — this script does not log in).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PANEL_VERSION="${PANEL_VERSION:-0.1.90}"
NODE_VERSION="${NODE_VERSION:-0.1.14}"
PANEL_IMAGE="${PANEL_IMAGE:-azamatbash/hapanel}"
NODE_IMAGE="${NODE_IMAGE:-azamatbash/hanode}"
PROD_CONFIG_ID="sha256:7d338bdaab2324dc6f9494d1c8beba4dd09f5034e9bc70f6fa02c8fe04c2b916"
PROD_HUB_DIGEST="sha256:e231c41702187a3e1be4a913d10bfeeb87e708b77ca53926dc74b044200efbc2"

if [[ "${REBUILD_PANEL:-}" == "1" ]]; then
  echo "==> REBUILD_PANEL=1 — building from sources (will NOT match prod digest)"
  # PANEL_VERSION is also stamped into the binary (GET /api/v1/config "version").
  docker build \
    --build-arg PANEL_VERSION="${PANEL_VERSION}" \
    -t "${PANEL_IMAGE}:${PANEL_VERSION}" \
    -t "${PANEL_IMAGE}:latest" \
    -f deploy/panel/Dockerfile \
    .
else
  echo "==> Tagging exact prod image ${PROD_CONFIG_ID} as ${PANEL_IMAGE}:${PANEL_VERSION} + :latest"
  if ! docker image inspect "${PROD_CONFIG_ID}" >/dev/null 2>&1; then
    echo "Prod image not local — pulling from Hub @ ${PROD_HUB_DIGEST}"
    docker pull "${PANEL_IMAGE}@${PROD_HUB_DIGEST}"
    docker tag "${PANEL_IMAGE}@${PROD_HUB_DIGEST}" "${PROD_CONFIG_ID}"
  fi
  docker tag "${PROD_CONFIG_ID}" "${PANEL_IMAGE}:${PANEL_VERSION}"
  docker tag "${PROD_CONFIG_ID}" "${PANEL_IMAGE}:latest"
fi

echo "==> Building ${NODE_IMAGE}:${NODE_VERSION}"
docker build \
  --build-arg AGENT_VERSION="${NODE_VERSION}" \
  -t "${NODE_IMAGE}:${NODE_VERSION}" \
  -t "${NODE_IMAGE}:latest" \
  -f deploy/node/agent/Dockerfile \
  .

echo "==> Pushing ${PANEL_IMAGE}:${PANEL_VERSION} and :latest"
docker push "${PANEL_IMAGE}:${PANEL_VERSION}"
docker push "${PANEL_IMAGE}:latest"

echo "==> Pushing ${NODE_IMAGE}:${NODE_VERSION} and :latest"
docker push "${NODE_IMAGE}:${NODE_VERSION}"
docker push "${NODE_IMAGE}:latest"

echo "Done."
echo "  ${PANEL_IMAGE}:${PANEL_VERSION}  ${PANEL_IMAGE}:latest  (expect Hub digest ${PROD_HUB_DIGEST})"
echo "  ${NODE_IMAGE}:${NODE_VERSION}   ${NODE_IMAGE}:latest"
