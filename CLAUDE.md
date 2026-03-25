# NoteBridge

Last verified: 2026-03-22

## Purpose

Drop-in replacement for Supernote Private Cloud (SPC). Single Go binary that handles device sync, file storage, OCR pipeline, CalDAV task sync, and a web UI. Designed for self-hosting with Docker.

## Tech Stack

- Language: Go 1.25
- Database: SQLite (modernc.org/sqlite, pure Go, no CGO)
- CalDAV: emersion/go-webdav
- Note parsing: jdkruzr/go-sn
- Deployment: Docker Compose
- Config: Environment variables with NB_ prefix

## Commands

- `go build ./cmd/notebridge/` - Build main binary
- `go build ./cmd/migrate/` - Build SPC migration tool
- `go test ./...` - Run all tests
- `./rebuild.sh` - Build and deploy via Docker Compose (requires .env)
- `./install.sh` - Interactive first-time setup
- `./migrate.sh` - Migrate data from SPC MariaDB to NoteBridge SQLite
- `./notebridge hash-password <pw>` - Generate bcrypt hash for web auth

## Project Structure

- `cmd/notebridge/` - Main entry point; wires all subsystems, runs two HTTP servers
- `cmd/migrate/` - SPC-to-NoteBridge migration CLI (MariaDB reader + engine)
- `internal/syncdb/` - SQLite schema and Store (all table operations)
- `internal/sync/` - Device sync API: auth (challenge-response, JWT, signed URLs), file CRUD, Socket.IO notifications
- `internal/blob/` - Blob storage interface + local filesystem implementation; ChunkStore for multipart uploads
- `internal/events/` - In-process pub/sub event bus (fire-and-forget goroutines)
- `internal/notestore/` - Note file inventory (notes table, SHA-256 tracking)
- `internal/search/` - FTS5 full-text search over note_content
- `internal/processor/` - OCR job queue: worker pool, LLM-based transcription, RECOGNTEXT injection via go-sn
- `internal/pipeline/` - File detection: subscribes to event bus + fsnotify + periodic reconciler
- `internal/taskstore/` - Task CRUD against schedule_tasks/schedule_groups tables
- `internal/caldav/` - CalDAV backend (VTODO conversion, go-webdav integration)
- `internal/auth/` - Basic Auth middleware for web UI (separate from device auth)
- `internal/logging/` - Structured slog with rotation (lumberjack), syslog, WebSocket broadcast
- `internal/config/` - Loads NB_ environment variables with defaults
- `internal/web/` - Web UI: file browser, search, task list, job management, log viewer

## Architecture

Two HTTP servers run concurrently:
- **Sync server** (default :19072) - Device-facing API matching SPC protocol
- **Web server** (default :8443) - Human-facing web UI + CalDAV endpoint

Both share the same SQLite database (single-writer, WAL mode) and event bus.

### Data Flow

1. Device uploads file via sync API -> blob stored on disk -> syncdb updated -> event published
2. Event bus notifies: pipeline (OCR queue), Socket.IO (device sync trigger)
3. Pipeline detects .note files -> enqueues OCR job -> processor transcribes -> injects RECOGNTEXT -> updates syncdb MD5 -> publishes FileModified event
4. CalDAV clients sync tasks via /caldav/ endpoint -> taskstore CRUD -> event triggers device sync

## Conventions

- All config via environment variables with NB_ prefix (see internal/config/)
- Device auth uses MD5 password hashes (SPC protocol compatibility)
- Web auth uses bcrypt password hashes
- Single-user design: one user bootstrapped at startup from NB_USER_EMAIL
- Snowflake IDs for files and user records
- Socket.IO messages match SPC ServerMessage format for device compatibility

## Key Decisions

- SQLite over MariaDB: Simpler deployment, no external DB dependency
- Pure-Go SQLite (modernc.org): No CGO, clean cross-compilation
- Single binary: All subsystems compiled into one executable
- Event bus over direct coupling: Pipeline, notifications, and CalDAV sync decoupled via events
- AfterInject hook: Processor calls back to update syncdb after RECOGNTEXT injection, avoiding circular imports

## Invariants

- SQLite MaxOpenConns=1: Single writer, WAL mode for concurrent reads
- Device sync protocol must match SPC exactly (auth flow, file API, Socket.IO format)
- Files table uses Snowflake IDs, never autoincrement
- Blob storage key = relative path from NB_STORAGE_PATH
- OCR pipeline backs up .note files before injection
- FTS5 index stays synchronized via SQLite triggers on note_content

## Required Environment Variables

- `NB_USER_EMAIL` - Bootstrap user email (device login)
- `NB_USER_PASSWORD_HASH` - MD5 hash of device password
- `NB_WEB_PASSWORD_HASH` - Bcrypt hash for web UI auth

## Boundaries

- Safe to edit: `internal/`, `cmd/`
- Generated at runtime: `*.db`, `/data/` directory
- Never commit: `.env`, binary artifacts (notebridge, migrate)
