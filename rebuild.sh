#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info() { printf '\033[1;34m==> %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m OK \033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m WARN \033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m FAIL \033[0m %s\n' "$*"; exit 1; }

if [[ ! -f "$SCRIPT_DIR/.env" ]]; then
    fail "No .env found. Run install.sh first."
fi

# --- fresh install option ---

DATA_DIR=$(grep -oP '^NB_DATA_DIR=\K.*' "$SCRIPT_DIR/.env" 2>/dev/null || echo "/data/notebridge")

if [[ "${1:-}" == "--fresh" || "${1:-}" == "-f" ]]; then
    warn "Fresh install requested. This will DELETE:"
    echo "  - Database: $DATA_DIR/notebridge.db"
    echo "  - Cache:    $DATA_DIR/cache/"
    echo "  - Backups:  $DATA_DIR/backups/"
    echo
    echo "  Storage ($DATA_DIR/storage/) will NOT be deleted."
    echo
    printf '  Type "yes" to confirm: '
    read -r confirm
    if [[ "$confirm" != "yes" ]]; then
        fail "Aborted."
    fi
    info "Stopping container..."
    sudo docker compose -f "$SCRIPT_DIR/docker-compose.yml" stop notebridge 2>/dev/null || true
    sudo rm -f "$DATA_DIR/notebridge.db" "$DATA_DIR/notebridge.db-wal" "$DATA_DIR/notebridge.db-shm"
    sudo rm -rf "$DATA_DIR/cache" "$DATA_DIR/backups"
    mkdir -p "$DATA_DIR/cache/chunks" "$DATA_DIR/backups"
    ok "Database and cache cleared"
elif [[ "${1:-}" == "--nuke" ]]; then
    warn "NUKE requested. This will DELETE EVERYTHING:"
    echo "  - Database: $DATA_DIR/notebridge.db"
    echo "  - Storage:  $DATA_DIR/storage/"
    echo "  - Cache:    $DATA_DIR/cache/"
    echo "  - Backups:  $DATA_DIR/backups/"
    echo
    printf '  Type "nuke" to confirm: '
    read -r confirm
    if [[ "$confirm" != "nuke" ]]; then
        fail "Aborted."
    fi
    info "Stopping container..."
    sudo docker compose -f "$SCRIPT_DIR/docker-compose.yml" stop notebridge 2>/dev/null || true
    sudo rm -rf "$DATA_DIR"
    mkdir -p "$DATA_DIR"/{storage,backups,cache/chunks}
    mkdir -p "$DATA_DIR"/storage/{DOCUMENT/Document,NOTE/Note,NOTE/MyStyle,EXPORT,SCREENSHOT,INBOX}
    ok "All data deleted"
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
