#!/usr/bin/env bash
# Build and push hapanel panel + node agent images to Docker Hub.
# Requires: docker login (run yourself — this script does not log in).
#
# Images:
#   azamatbash/hapanel:0.1.0  + :latest
#   azamatbash/hanode:0.1.0   + :latest
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${VERSION:-0.1.0}"
PANEL_IMAGE="${PANEL_IMAGE:-azamatbash/hapanel}"
NODE_IMAGE="${NODE_IMAGE:-azamatbash/hanode}"

echo "==> Building ${PANEL_IMAGE}:${VERSION}"
docker build \
  -t "${PANEL_IMAGE}:${VERSION}" \
  -t "${PANEL_IMAGE}:latest" \
  -f deploy/panel/Dockerfile \
  .

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
echo "  ${PANEL_IMAGE}:${VERSION}  ${PANEL_IMAGE}:latest"
echo "  ${NODE_IMAGE}:${VERSION}   ${NODE_IMAGE}:latest"
