# NoteBridge

Self-hosted replacement for Supernote Private Cloud. Single Go binary, SQLite storage, Docker deployment.

Syncs files with Supernote tablets using the same protocol as SPC, adds full-text search via LLM-powered OCR, and exposes tasks to any CalDAV client.

## Features

- **Device sync** — Drop-in SPC replacement. Challenge-response auth, file CRUD, Socket.IO push notifications.
- **OCR pipeline** — Detects `.note` files, transcribes handwriting via configurable LLM, injects searchable text back into the file.
- **Full-text search** — FTS5 index over transcribed note content.
- **CalDAV** — Two-way task sync with Thunderbird, Apple Reminders, or any VTODO client.
- **Web UI** — File browser, search, task list, OCR job management, live log viewer.
- **Migration tool** — Import users, files, and tasks from an existing SPC MariaDB instance.

## Quick Start

```bash
git clone https://github.com/jdkruzr/notebridge.git
cd notebridge
./install.sh
```

The installer prompts for your email, device password, and web UI password, then builds and starts the container.

After install, point your Supernote tablet's Private Cloud setting to `http://<your-server>:19072`.

## Requirements

- Docker with Compose v2
- Go 1.24+ (only if building outside Docker)

## Architecture

Single HTTP server on port 19072 (matching SPC's port), routing by path:

| Path | Purpose |
|------|---------|
| `/api/*` | Device sync API (SPC protocol) |
| `/socket.io/*` | WebSocket push notifications |
| `/caldav/*` | CalDAV task sync (Basic Auth) |
| `/*` | Web UI (Basic Auth) |

All backed by a single SQLite database (WAL mode) and an in-process event bus.

```
Device  ──sync API──▶  NoteBridge  ──event bus──▶  OCR Pipeline
                           │                            │
                       SQLite DB ◀──────────────────────┘
                           │
CalDAV client  ◀──/caldav/─┘
```

## Configuration

All settings via environment variables with `NB_` prefix. The installer writes these to `.env`.

| Variable | Required | Description |
|----------|----------|-------------|
| `NB_USER_EMAIL` | Yes | Device login email |
| `NB_USER_PASSWORD_HASH` | Yes | MD5 hash of device password |
| `NB_WEB_PASSWORD_HASH` | Yes | Bcrypt hash for web UI |
| `NB_STORAGE_PATH` | No | Blob storage directory (default: `/data/storage`) |
| `NB_DB_PATH` | No | SQLite database path (default: `/data/notebridge.db`) |
| `NB_SYNC_PORT` | No | Server port (default: `19072`) |
| `NB_LOG_LEVEL` | No | Log level: debug, info, warn, error (default: `info`) |

## Migrating from SPC

If you have an existing Supernote Private Cloud installation:

```bash
./migrate.sh
```

This reads from the SPC MariaDB database and copies users, files, folder structure, and tasks into NoteBridge's SQLite database and blob storage.

## Development

```bash
# Build
go build ./cmd/notebridge/

# Run tests
go test ./...

# Build migration tool
go build ./cmd/migrate/

# Deploy
./rebuild.sh
```

## Project Structure

```
cmd/notebridge/     Main binary — wires all subsystems
cmd/migrate/        SPC migration CLI
internal/
  sync/             Device sync API (auth, files, Socket.IO)
  syncdb/           SQLite schema and data access
  blob/             File storage (local filesystem)
  events/           In-process pub/sub event bus
  pipeline/         File detection (event bus + fsnotify + reconciler)
  processor/        OCR job queue and LLM transcription
  notestore/        Note file inventory and SHA-256 tracking
  search/           FTS5 full-text search
  taskstore/        Task CRUD
  caldav/           CalDAV backend (VTODO conversion)
  web/              Web UI handlers
  auth/             Basic Auth middleware for web UI
  config/           Environment variable loader
  logging/          Structured logging with rotation
```

## License

See [LICENSE](LICENSE) for details.
