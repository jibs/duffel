#!/usr/bin/env bash
# Production startup script for the duffel systemd service.
# Called by systemd on every start/restart — pulls latest from origin/main,
# rebuilds frontend + backend, then execs the binary (replacing this process).
set -euo pipefail

export PATH=/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

cd "${DUFFEL_APP_DIR:-/opt/duffel}"

# Snapshot dependency manifests before pulling
old_pkg=$(md5sum pnpm-lock.yaml 2>/dev/null || echo "none")

git fetch origin
git reset --hard origin/main

# Only reinstall deps (and rebuild native addons) when lockfile changed
new_pkg=$(md5sum pnpm-lock.yaml 2>/dev/null || echo "none")
if [ "$old_pkg" != "$new_pkg" ]; then
  echo "pnpm-lock.yaml changed — running make setup"
  make setup
fi

make build-js
make build

exec ./duffel
