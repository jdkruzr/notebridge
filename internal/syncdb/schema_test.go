package syncdb

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("Open returned nil db")
	}
}

func TestAllTablesExist(t *testing.T) {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Tables that should exist
	tables := []string{
		// Auth & Users
		"users", "equipment", "auth_tokens", "login_challenges",
		"sync_locks", "server_settings", "url_nonces",
		// File Catalog
		"files", "recycle_files", "chunk_uploads",
		// Tasks
		"schedule_groups", "schedule_tasks",
		// Digests
		"summaries",
		// Notes Pipeline
		"notes", "jobs", "note_content", "note_fts",
	}

	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := db.QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
				table,
			).Scan(&name)
			if err != nil {
				if err == sql.ErrNoRows {
					t.Errorf("table %s not found", table)
				} else {
					t.Errorf("query failed: %v", err)
				}
			}
		})
	}
}

func TestWALModeEnabled(t *testing.T) {
	t.Helper()

	// Use temporary file-based database to test WAL mode
	// (in-memory databases don't support WAL mode)
	tmpdir := t.TempDir()
	dbpath := filepath.Join(tmpdir, "test.db")

	db, err := Open(dbpath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	var mode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		t.Fatalf("PRAGMA query failed: %v", err)
	}

	if mode != "wal" {
		t.Errorf("journal_mode is %s, want wal", mode)
	}
}

func TestSchemaIdempotent(t *testing.T) {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	// Call ensureSchema again
	err = ensureSchema(db)
	if err != nil {
		t.Errorf("calling ensureSchema twice failed: %v", err)
	}

	// Verify tables still exist
	var count int
	err = db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}

	if count == 0 {
		t.Error("no tables found after second ensureSchema call")
	}
}

func TestIndexesExist(t *testing.T) {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	indexes := []string{
		"idx_files_user_dir",
		"idx_summaries_user",
	}

	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var name string
			err := db.QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?",
				idx,
			).Scan(&name)
			if err != nil {
				if err == sql.ErrNoRows {
					t.Errorf("index %s not found", idx)
				} else {
					t.Errorf("query failed: %v", err)
				}
			}
		})
	}
}

func TestFTS5TriggersExist(t *testing.T) {
	t.Helper()

	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	triggers := []string{
		"note_content_ai",
		"note_content_ad",
		"note_content_au",
	}

	for _, trigger := range triggers {
		t.Run(trigger, func(t *testing.T) {
			var name string
			err := db.QueryRow(
				"SELECT name FROM sqlite_master WHERE type='trigger' AND name=?",
				trigger,
			).Scan(&name)
			if err != nil {
				if err == sql.ErrNoRows {
					t.Errorf("trigger %s not found", trigger)
				} else {
					t.Errorf("query failed: %v", err)
				}
			}
		})
	}
}
