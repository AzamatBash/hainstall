#!/usr/bin/env bash
# Push panel/node images to Docker Hub.
# Panel: by default re-pushes the exact production image (see deploy/panel/PROD_IMAGE.txt).
# Set REBUILD_PANEL=1 to build from deploy/panel/Dockerfile (dev only — digest will differ).
# Requires: docker login (run yourself — this script does not log in).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0}"
PANEL_IMAGE="${PANEL_IMAGE:-azamatbash/hapanel}"
NODE_IMAGE="${NODE_IMAGE:-azamatbash/hanode}"
PROD_CONFIG_ID="sha256:521209be3b6e1cf29dc3dae3e8bb7cee428ea7fec98a28646d90e48d2382bdb1"
PROD_HUB_DIGEST="sha256:99a718b5cdb7448c13baa6ee54d33ecd8a8879c97c4beebf4bd4fa87a748f7ee"

if [[ "${REBUILD_PANEL:-}" == "1" ]]; then
  echo "==> REBUILD_PANEL=1 — building from sources (will NOT match prod digest)"
  docker build \
    -t "${PANEL_IMAGE}:${VERSION}" \
    -t "${PANEL_IMAGE}:latest" \
    -f deploy/panel/Dockerfile \
    .
else
  echo "==> Tagging exact prod image ${PROD_CONFIG_ID} as ${PANEL_IMAGE}:${VERSION} + :latest"
  if ! docker image inspect "${PROD_CONFIG_ID}" >/dev/null 2>&1; then
    echo "Prod image not local — pulling from Hub @ ${PROD_HUB_DIGEST}"
    docker pull "${PANEL_IMAGE}@${PROD_HUB_DIGEST}"
    docker tag "${PANEL_IMAGE}@${PROD_HUB_DIGEST}" "${PROD_CONFIG_ID}"
  fi
  docker tag "${PROD_CONFIG_ID}" "${PANEL_IMAGE}:${VERSION}"
  docker tag "${PROD_CONFIG_ID}" "${PANEL_IMAGE}:latest"
fi

echo "==> Building ${NODE_IMAGE}:${VERSION}"
docker build \
  -t "${NODE_IMAGE}:${VERSION}" \
  -t "${NODE_IMAGE}:latest" \
  -f deploy/node/agent/Dockerfile \
  .

echo "==> Pushing ${PANEL_IMAGE}:${VERSION} and :latest"
docker push "${PANEL_IMAGE}:${VERSION}"
docker push "${PANEL_IMAGE}:latest"

echo "==> Pushing ${NODE_IMAGE}:${VERSION} and :latest"
docker push "${NODE_IMAGE}:${VERSION}"
docker push "${NODE_IMAGE}:latest"

echo "Done."
echo "  ${PANEL_IMAGE}:${VERSION}  ${PANEL_IMAGE}:latest  (expect Hub digest ${PROD_HUB_DIGEST})"
echo "  ${NODE_IMAGE}:${VERSION}   ${NODE_IMAGE}:latest"
