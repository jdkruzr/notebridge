#!/usr/bin/env bash
set -euo pipefail

# NoteBridge Interactive Installer
# Safe to re-run: overwrites generated config files each time.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEFAULT_DATA_DIR="/data/notebridge"
DEFAULT_PORT="19072"

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
prompt SYNC_PORT "Server port" "$DEFAULT_PORT"
prompt BASE_URL "External URL (tablet connects to this)" ""
prompt USER_EMAIL "User email" ""
prompt_password USER_PASSWORD "Device password"
prompt WEB_USERNAME "Web UI username" "admin"
prompt_password WEB_PASSWORD "Web UI password"
prompt CALDAV_COLLECTION "CalDAV task collection name" "Tasks"

echo
info "SPC Migration (leave blank for new installs)"
prompt SPC_USER_ID "SPC user ID (from u_user.user_id)" ""
prompt SPC_MACHINE_ID "SPC machine ID (from t_machine_id)" ""

echo
info "OCR Pipeline (leave blank to disable)"
prompt OCR_API_KEY "LLM API key (Anthropic, OpenAI, etc.)" ""
if [[ -n "$OCR_API_KEY" ]]; then
    prompt OCR_API_URL "LLM API URL" "https://api.anthropic.com/v1/messages"
    prompt OCR_FORMAT "API format (anthropic or openai)" "anthropic"
    prompt OCR_MODEL "Model name" "claude-sonnet-4-20250514"
    OCR_ENABLED="true"
else
    OCR_API_URL=""
    OCR_FORMAT=""
    OCR_MODEL=""
    OCR_ENABLED="false"
fi

echo

# --- generate secrets ---

info "Generating secrets"

# MD5 hash of device password (matching Supernote device protocol)
USER_PASSWORD_HASH=$(echo -n "$USER_PASSWORD" | md5sum | awk '{print $1}')
ok "Device password MD5 hash computed"

# Bcrypt hash of web password
# Use the notebridge binary to generate the hash
cd "$SCRIPT_DIR"
WEB_PASSWORD_HASH=$(./notebridge hash-password "$WEB_PASSWORD" 2>/dev/null || echo "")
if [[ -z "$WEB_PASSWORD_HASH" ]]; then
    # Fallback: try to build it first
    info "Building notebridge for password hashing..."
    go build -o notebridge ./cmd/notebridge/ || fail "Failed to build notebridge"
    WEB_PASSWORD_HASH=$(./notebridge hash-password "$WEB_PASSWORD")
fi
ok "Web password bcrypt hash generated"

# JWT secret: 32 bytes random hex
JWT_SECRET=$(openssl rand -hex 32)
ok "JWT secret generated"

echo

# --- create directories ---

info "Creating data directories"
mkdir -p "$DATA_DIR"/{storage,backups,cache/chunks}
mkdir -p "$DATA_DIR"/storage/{DOCUMENT/Document,NOTE/Note,NOTE/MyStyle,EXPORT,SCREENSHOT,INBOX}
ok "Directories created at $DATA_DIR"

echo

# --- write .env ---

info "Writing configuration"

# .env is used by Docker Compose for YAML variable interpolation (ports, volumes).
# notebridge.env is passed to the container via env_file. Docker Compose
# interprets $ in env_file values too, so we must double $ → $$ for any
# value that contains literal $ (bcrypt hashes, JWT secrets).

escape_dollars() { sed 's/\$/\$\$/g' <<<"$1"; }

cat > "$SCRIPT_DIR/.env" <<EOF
NB_DATA_DIR=$DATA_DIR
NB_SYNC_PORT=$SYNC_PORT
EOF

cat > "$SCRIPT_DIR/notebridge.env" <<ENVEOF
NB_DB_PATH=$DATA_DIR/notebridge.db
NB_STORAGE_PATH=$DATA_DIR/storage
NB_BACKUP_PATH=$DATA_DIR/backups
NB_CACHE_PATH=$DATA_DIR/cache
NB_BASE_URL=$BASE_URL
NB_SYNC_LISTEN_ADDR=:$SYNC_PORT
NB_LOG_LEVEL=info
NB_LOG_FORMAT=json
NB_JWT_SECRET=$(escape_dollars "$JWT_SECRET")
NB_USER_EMAIL=$USER_EMAIL
NB_USER_PASSWORD_HASH=$USER_PASSWORD_HASH
NB_USER_ID=$SPC_USER_ID
NB_MACHINE_ID=$SPC_MACHINE_ID
NB_CALDAV_COLLECTION_NAME=$CALDAV_COLLECTION
NB_WEB_USERNAME=$WEB_USERNAME
NB_WEB_PASSWORD_HASH=$(escape_dollars "$WEB_PASSWORD_HASH")
NB_OCR_ENABLED=$OCR_ENABLED
NB_OCR_API_URL=$OCR_API_URL
NB_OCR_API_KEY=$OCR_API_KEY
NB_OCR_FORMAT=$OCR_FORMAT
NB_OCR_MODEL=$OCR_MODEL
ENVEOF
ok ".env and notebridge.env created"

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
echo "Server: http://localhost:${SYNC_PORT}"
echo "Web UI: http://localhost:${SYNC_PORT} (Basic Auth: ${WEB_USERNAME})"
echo "Device sync: point your Supernote to http://<this-server>:${SYNC_PORT}"
