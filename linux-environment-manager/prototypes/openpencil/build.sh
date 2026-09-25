#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CACHE_DIR="${OPENPENCIL_NPM_CACHE:-/tmp/opencode/openpencil-npm-cache}"
mkdir -p "$CACHE_DIR" "$ROOT/preview"

# OpenPencil's supported editable format is FIG. The builder uses the CLI's
# headless Figma-compatible API, so no desktop window or MCP connection is needed.
docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e HOME=/tmp \
  -e npm_config_cache=/tmp/npm-cache \
  -v "$CACHE_DIR:/tmp/npm-cache" \
  -v "$ROOT:/work" \
  -v "$ROOT/source/build.js:/tmp/build.js:ro" \
  -w /work \
  node:24-bookworm-slim \
  sh -lc '
    set -eu
    printf "<!doctype html><html><body></body></html>" > /tmp/openpencil-base.html
    npx --yes --package=@open-pencil/cli@0.15.1 openpencil import /tmp/openpencil-base.html --format fig --output /tmp/openpencil-base.fig --json >/dev/null
    npx --yes --package=@open-pencil/cli@0.15.1 openpencil eval /tmp/openpencil-base.fig --stdin -o /work/lem-dashboard.fig < /tmp/build.js
    npx --yes --package=@open-pencil/cli@0.15.1 openpencil export /work/lem-dashboard.fig -f png -o /work/preview/lem-dashboard.png
  '

printf 'OpenPencil prototype generated:\n  %s\n  %s\n' \
  "$ROOT/lem-dashboard.fig" \
  "$ROOT/preview/lem-dashboard.png"
