#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

info() { printf '\033[1;34m==> %s\033[0m\n' "$*"; }
ok()   { printf '\033[1;32m OK \033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m WARN \033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m FAIL \033[0m %s\n' "$*"; exit 1; }

info "NoteBridge Migration Tool"
echo "This migrates data from Supernote Private Cloud to NoteBridge."
echo

# Read SPC .dbenv for MariaDB credentials
SPC_DBENV="${SPC_DBENV:-/mnt/supernote/.dbenv}"
if [[ -f "$SPC_DBENV" ]]; then
    info "Reading SPC credentials from $SPC_DBENV"
    # Parse .dbenv format
    DB_HOST=$(grep -oP 'DB_HOST=\K.*' "$SPC_DBENV" || echo "localhost")
    DB_PORT=$(grep -oP 'DB_PORT=\K.*' "$SPC_DBENV" || echo "3306")
    DB_NAME=$(grep -oP 'DB_NAME=\K.*' "$SPC_DBENV" || echo "supernotedb")
    DB_USER=$(grep -oP 'DB_USER=\K.*' "$SPC_DBENV" || echo "enote")
    DB_PASS=$(grep -oP 'DB_PASS=\K.*' "$SPC_DBENV" || echo "")
else
    warn "No .dbenv found. Enter SPC MariaDB credentials manually."
    read -rp "MariaDB host [localhost]: " DB_HOST; DB_HOST=${DB_HOST:-localhost}
    read -rp "MariaDB port [3306]: " DB_PORT; DB_PORT=${DB_PORT:-3306}
    read -rp "Database name [supernotedb]: " DB_NAME; DB_NAME=${DB_NAME:-supernotedb}
    read -rp "Database user [enote]: " DB_USER; DB_USER=${DB_USER:-enote}
    read -rsp "Database password: " DB_PASS; echo
fi

SPC_PATH="${SPC_PATH:-/mnt/supernote/supernote_data}"
NB_DATA="${NB_DATA:-/data/notebridge}"

info "Building migration tool..."
go build -C "$SCRIPT_DIR" -o "$SCRIPT_DIR/migrate" ./cmd/migrate/ || fail "Build failed"

info "Running migration..."
"$SCRIPT_DIR/migrate" \
    -spc-dsn "${DB_USER}:${DB_PASS}@tcp(${DB_HOST}:${DB_PORT})/${DB_NAME}" \
    -spc-path "$SPC_PATH" \
    -nb-db "${NB_DATA}/notebridge.db" \
    -nb-storage "${NB_DATA}/storage"

ok "Migration complete!"
echo
echo "Next steps:"
echo "  1. Start NoteBridge: cd $SCRIPT_DIR && ./rebuild.sh"
echo "  2. Point your tablet at NoteBridge's IP address"
echo "  3. Sync and verify everything transferred correctly"
