#!/usr/bin/env bash
# Push panel/node images to Docker Hub.
# Panel: by default re-pushes the exact production image (see deploy/panel/PROD_IMAGE.txt).
# Set REBUILD_PANEL=1 to build from deploy/panel/Dockerfile (dev only — digest will differ).
# Requires: docker login (run yourself — this script does not log in).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PANEL_VERSION="${PANEL_VERSION:-0.1.76}"
NODE_VERSION="${NODE_VERSION:-0.1.13}"
PANEL_IMAGE="${PANEL_IMAGE:-azamatbash/hapanel}"
NODE_IMAGE="${NODE_IMAGE:-azamatbash/hanode}"
PROD_CONFIG_ID="sha256:38edfe3fb7de4955d092f118e4e1319a4446983de5a6e89c98f47d1f87afb76f"
PROD_HUB_DIGEST="sha256:7877fa4a35aceed715fec7a42de4acba89e90be3e196b49e996802c838ca7d99"

if [[ "${REBUILD_PANEL:-}" == "1" ]]; then
  echo "==> REBUILD_PANEL=1 — building from sources (will NOT match prod digest)"
  docker build \
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
