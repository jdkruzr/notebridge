# NoteBridge SPC Replacement — Human Test Plan

Generated: 2026-03-22
Implementation Plan: `docs/implementation-plans/2026-03-22-notebridge-spc-replacement/`

## Overview

This test plan covers manual verification steps that cannot be fully validated by automated tests. It is organized into four verification phases plus an end-to-end lifecycle scenario. Automated tests (244+ across 12 packages) cover unit and integration logic; this plan targets deployment, device interaction, pipeline behavior, and migration fidelity.

---

## Phase 1: Deployment Verification

### 1.1 Docker Build & Startup

- [ ] Run `./install.sh` on a clean machine — verify it prompts for email, device password, and web password
- [ ] Verify `.env` is created with `NB_USER_EMAIL`, `NB_USER_PASSWORD_HASH`, `NB_WEB_PASSWORD_HASH`
- [ ] Run `./rebuild.sh` — verify Docker image builds successfully (CGO_ENABLED=0)
- [ ] Verify container starts and both servers bind:
  - Sync server on port 19072
  - Web server on port 8443
- [ ] Verify SQLite database is created at `/data/notebridge.db` with WAL mode
- [ ] Verify storage directory exists at `/data/storage/`
- [ ] Check logs for clean startup (no errors, all subsystems initialized)

### 1.2 Environment Variable Configuration

- [ ] Override `NB_SYNC_PORT` and `NB_WEB_PORT` — verify servers bind to custom ports
- [ ] Set `NB_LOG_LEVEL=debug` — verify debug output appears
- [ ] Verify `NB_STORAGE_PATH` override changes blob storage location
- [ ] Verify missing required env vars (`NB_USER_EMAIL`) cause a clear error on startup

---

## Phase 2: Device Sync Protocol

### 2.1 Authentication Flow

- [ ] From Supernote tablet, configure Private Cloud with NoteBridge server address
- [ ] Attempt login with correct credentials — verify device receives JWT token
- [ ] Attempt login with wrong password — verify device shows error (E0019)
- [ ] Attempt 6+ wrong logins — verify account lockout (E0045), then recovers after lockout window

### 2.2 File Sync Operations

- [ ] Create a new note on device — trigger sync — verify file appears in NoteBridge storage
- [ ] Verify file metadata (MD5, size, timestamps) matches device expectations
- [ ] Modify note on device — re-sync — verify updated file replaces old version
- [ ] Delete a file on device — sync — verify server marks file as deleted
- [ ] Create a folder on device — sync — verify folder structure is preserved

### 2.3 Socket.IO Notifications

- [ ] After uploading a file via sync API, verify device receives Socket.IO push notification
- [ ] Verify Engine.IO v3 handshake (polling upgrade to WebSocket) works with tablet
- [ ] Verify Socket.IO reconnection after network interruption

### 2.4 Multi-Device Scenario

- [ ] Sync from Device A, then sync from Device B — verify both see complete file list
- [ ] Upload from Device A, verify Device B receives the file on next sync

---

## Phase 3: OCR Pipeline & Search

### 3.1 OCR Processing

- [ ] Upload a `.note` file with handwritten content
- [ ] Verify pipeline detects the file and enqueues an OCR job
- [ ] Verify processor picks up the job and calls the configured LLM endpoint
- [ ] Verify RECOGNTEXT is injected into the `.note` file (FILE_RECOGN_TYPE=0)
- [ ] Verify the original `.note` is backed up before injection
- [ ] Verify syncdb MD5 is updated after injection
- [ ] Verify `FileModifiedEvent` is published (triggers device re-sync)

### 3.2 Search Integration

- [ ] After OCR completes, search for a word from the handwritten content via web UI
- [ ] Verify FTS5 search returns the correct note with highlighted snippet
- [ ] Upload a second note, verify search distinguishes between notes
- [ ] Delete a note — verify its content is removed from search index

### 3.3 Pipeline Resilience

- [ ] Restart the container while an OCR job is in progress — verify job is re-enqueued on startup (reconciler)
- [ ] Verify fsnotify detects new `.note` files placed directly in storage (not via sync API)
- [ ] Verify hash-based change detection skips files that haven't changed

---

## Phase 4: Migration from SPC

### 4.1 Migration Tool

- [ ] Run `./migrate.sh` pointing at an SPC MariaDB instance
- [ ] Verify all users are migrated with correct email and password hashes
- [ ] Verify file records are created with correct paths (folder tree reconstructed)
- [ ] Verify blob files are copied to NoteBridge storage with MD5 verification
- [ ] Verify tasks are migrated with correct timestamps and completion status
- [ ] Verify task groups preserve their structure

### 4.2 Post-Migration Verification

- [ ] After migration, log in from device — verify file list matches SPC
- [ ] Download a migrated file — verify content is identical to SPC original
- [ ] Verify migrated tasks appear in CalDAV client
- [ ] Verify search works on migrated content (if OCR was previously applied)

---

## Phase 5: End-to-End Lifecycle

This scenario validates the complete workflow from fresh install through daily use:

1. [ ] Fresh install via `install.sh`
2. [ ] Start with `rebuild.sh`
3. [ ] Configure Supernote tablet to point at NoteBridge
4. [ ] Login from tablet (challenge-response auth)
5. [ ] Create a handwritten note on tablet
6. [ ] Sync note to NoteBridge
7. [ ] Verify OCR pipeline processes the note
8. [ ] Search for handwritten text via web UI — find the note
9. [ ] Create a task on tablet
10. [ ] Sync task to NoteBridge
11. [ ] Open CalDAV client (e.g., Thunderbird) — verify task appears
12. [ ] Complete task in CalDAV client
13. [ ] Sync tablet — verify task shows as completed
14. [ ] Access web UI file browser — verify note is listed
15. [ ] View note in web UI — verify rendered content
16. [ ] Restart container — verify all data persists
17. [ ] Re-sync tablet — verify no data loss or duplication

---

## Acceptance Criteria Cross-Reference

| AC | Description | Automated | Manual Phase |
|----|-------------|-----------|--------------|
| AC1.1-1.4 | Auth challenge-response, JWT, errors | Yes | 2.1 |
| AC1.5 | Account lockout after 6 failures | Yes | 2.1 |
| AC1.6 | Expired challenge rejection | Yes | — |
| AC2.x | File CRUD operations | Yes | 2.2 |
| AC3.x | Socket.IO notifications | Partial | 2.3 |
| AC4.x | OCR pipeline processing | Partial (skipped) | 3.1, 3.2 |
| AC5.x | Task/digest CRUD | Yes | — |
| AC6.x | Rate limiting, middleware | Yes | — |
| AC7.x | CalDAV sync | Yes | 4.2, 5 |
| AC8.x | Web UI | Yes | 3.2, 5 |
| AC9.x | Migration tool | Yes | 4.1, 4.2 |

**Note:** AC4.x tests are mostly skipped in automation (require mock OCR endpoint and `.note` fixtures). Manual verification in Phase 3 is critical for these criteria.
