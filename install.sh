#!/usr/bin/env bash
set -euo pipefail

# NoteBridge Interactive Installer
# Safe to re-run: overwrites generated config files each time.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_DATA_DIR="/data/notebridge"
DEFAULT_WEB_PORT="8443"
DEFAULT_SYNC_PORT="19071"

# --- helpers ---

info()  { printf '\033[1;34m==> %s\033[0m\n' "$*"; }
ok()    { printf '\033[1;32m OK \033[0m %s\n' "$*"; }
warn()  { printf '\033[1;33m WARN \033[0m %s\n' "$*"; }
fail()  { printf '\033[1;31m FAIL \033[0m %s\n' "$*"; exit 1; }

prompt() {
    local var="$1" msg="$2" default="$3"
    local input
    if [[ -n "$default" ]]; then
        printf '%s [%s]: ' "$msg" "$default"
    else
        printf '%s: ' "$msg"
    fi
    read -r input
    eval "$var=\"${input:-$default}\""
}

prompt_password() {
    local var="$1" msg="$2"
    local pw1 pw2
    while true; do
        printf '%s: ' "$msg"
        read -rs pw1
        echo
        printf 'Confirm password: '
        read -rs pw2
        echo
        if [[ "$pw1" == "$pw2" ]]; then
            if [[ -z "$pw1" ]]; then
                warn "Password cannot be empty. Try again."
                continue
            fi
            eval "$var=\"$pw1\""
            return
        fi
        warn "Passwords don't match. Try again."
    done
}

# --- pre-flight checks ---

info "NoteBridge Installer"
echo

# Docker
if ! command -v docker &>/dev/null; then
    fail "Docker is not installed. Install Docker first."
fi
ok "Docker found"

# Docker Compose
if ! docker compose version &>/dev/null; then
    fail "Docker Compose (v2) not found. Install docker-compose-plugin."
fi
ok "Docker Compose found"

echo

# --- configuration ---

info "Configuration"
echo

prompt DATA_DIR "Data directory" "$DEFAULT_DATA_DIR"
prompt WEB_PORT "Web UI port" "$DEFAULT_WEB_PORT"
prompt SYNC_PORT "Device sync API port" "$DEFAULT_SYNC_PORT"
prompt USER_EMAIL "User email" ""
prompt_password USER_PASSWORD "Password"

echo

# --- generate secrets ---

info "Generating secrets"

# MD5 hash of password (matching Supernote device protocol)
USER_PASSWORD_HASH=$(echo -n "$USER_PASSWORD" | md5sum | awk '{print $1}')
ok "Password MD5 hash computed"

# JWT secret: 32 bytes random hex
JWT_SECRET=$(openssl rand -hex 32)
ok "JWT secret generated"

echo

# --- create directories ---

info "Creating data directories"
mkdir -p "$DATA_DIR"/{storage,backups,cache}
ok "Directories created at $DATA_DIR"

echo

# --- write .env ---

info "Writing configuration to .env"
cat > "$SCRIPT_DIR/.env" <<EOF
NB_DATA_DIR=$DATA_DIR
NB_WEB_PORT=$WEB_PORT
NB_SYNC_PORT=$SYNC_PORT
NB_DB_PATH=$DATA_DIR/notebridge.db
NB_STORAGE_PATH=$DATA_DIR/storage
NB_BACKUP_PATH=$DATA_DIR/backups
NB_CACHE_PATH=$DATA_DIR/cache
NB_WEB_LISTEN_ADDR=:8443
NB_SYNC_LISTEN_ADDR=:19071
NB_LOG_LEVEL=info
NB_LOG_FORMAT=json
NB_JWT_SECRET=$JWT_SECRET
NB_USER_EMAIL=$USER_EMAIL
NB_USER_PASSWORD_HASH=$USER_PASSWORD_HASH
EOF
ok ".env created"

echo

# --- build and start ---

info "Building and starting NoteBridge..."
sudo docker compose -f "$SCRIPT_DIR/docker-compose.yml" up -d --build \
    || fail "Build/start failed"
ok "Container started"

echo

# --- health check ---

info "Checking health..."
sleep 2
if curl -sf "http://localhost:${SYNC_PORT}/health" >/dev/null 2>&1; then
    ok "Health check passed"
else
    sleep 3
    if curl -sf "http://localhost:${SYNC_PORT}/health" >/dev/null 2>&1; then
        ok "Health check passed"
    else
        warn "Health check failed. Run: sudo docker logs notebridge"
    fi
fi

echo

info "NoteBridge installed successfully!"
echo "Web UI: http://localhost:${WEB_PORT}"
echo "Sync API: http://localhost:${SYNC_PORT}"
