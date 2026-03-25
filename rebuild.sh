#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info() { printf '\033[1;34m==> %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m OK \033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m FAIL \033[0m %s\n' "$*"; exit 1; }

if [[ ! -f "$SCRIPT_DIR/.env" ]]; then
    fail "No .env found. Run install.sh first."
fi

info "Building and restarting NoteBridge..."
sudo docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d --build --force-recreate notebridge \
    || fail "Build/restart failed"
ok "Container running"

# Read port from .env or default
PORT=$(grep -oP '^NB_SYNC_PORT=\K.*' "$SCRIPT_DIR/.env" 2>/dev/null || echo "19072")

sleep 2
if curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
    ok "Health check passed"
else
    sleep 3
    if curl -sf "http://localhost:${PORT}/health" >/dev/null 2>&1; then
        ok "Health check passed"
    else
        fail "Health check failed. Run: sudo docker logs notebridge"
    fi
fi

info "Done!"
