package syncdb

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open opens a SQLite database with WAL mode and creates schema if needed.
// Uses modernc.org/sqlite driver (pure Go, no CGO required).
func Open(path string) (*sql.DB, error) {
	// Open database
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set single-writer mode (matching UltraBridge's notedb pattern)
	db.SetMaxOpenConns(1)

	// Enable WAL mode for better concurrent reads
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Create schema
	if err := ensureSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ensure schema: %w", err)
	}

	return db, nil
}

// ensureSchema creates all tables if they don't exist.
func ensureSchema(db *sql.DB) error {
	schema := `
-- Auth & Users
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	email TEXT UNIQUE NOT NULL,
	password_hash TEXT NOT NULL,
	username TEXT,
	error_count INTEGER DEFAULT 0,
	last_error_at DATETIME,
	locked_until DATETIME
);

CREATE TABLE IF NOT EXISTS equipment (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	equipment_no TEXT,
	user_id INTEGER NOT NULL,
	name TEXT,
	status TEXT,
	total_capacity INTEGER,
	UNIQUE(equipment_no, user_id),
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS auth_tokens (
	key TEXT PRIMARY KEY,
	token TEXT NOT NULL,
	user_id INTEGER NOT NULL,
	equipment_no TEXT,
	expires_at DATETIME,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS login_challenges (
	account TEXT NOT NULL,
	timestamp DATETIME NOT NULL,
	random_code TEXT NOT NULL,
	PRIMARY KEY (account, timestamp)
);

CREATE TABLE IF NOT EXISTS sync_locks (
	user_id INTEGER PRIMARY KEY,
	equipment_no TEXT,
	expires_at DATETIME,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS server_settings (
	key TEXT PRIMARY KEY,
	value TEXT
);

CREATE TABLE IF NOT EXISTS url_nonces (
	nonce TEXT PRIMARY KEY,
	expires_at DATETIME NOT NULL
);

-- File Catalog (NOT NULL DEFAULT on all columns to prevent NULL scan errors)
CREATE TABLE IF NOT EXISTS files (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	directory_id INTEGER NOT NULL DEFAULT 0,
	file_name TEXT NOT NULL,
	inner_name TEXT NOT NULL DEFAULT '',
	storage_key TEXT NOT NULL DEFAULT '',
	md5 TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	is_folder TEXT NOT NULL DEFAULT 'N',
	is_active TEXT NOT NULL DEFAULT 'Y',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS recycle_files (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	directory_id INTEGER NOT NULL DEFAULT 0,
	file_name TEXT NOT NULL,
	inner_name TEXT NOT NULL DEFAULT '',
	storage_key TEXT NOT NULL DEFAULT '',
	md5 TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	is_folder TEXT NOT NULL DEFAULT 'N',
	is_active TEXT NOT NULL DEFAULT 'Y',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	deleted_at DATETIME,
	original_directory_id INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS chunk_uploads (
	upload_id TEXT NOT NULL,
	part_number INTEGER NOT NULL,
	total_chunks INTEGER,
	chunk_md5 TEXT,
	path TEXT,
	PRIMARY KEY (upload_id, part_number)
);

-- Tasks
CREATE TABLE IF NOT EXISTS schedule_groups (
	task_list_id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	title TEXT,
	last_modified DATETIME,
	create_time DATETIME,
	updated_at DATETIME,
	FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS schedule_tasks (
	task_id TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL,
	task_list_id TEXT NOT NULL,
	title TEXT,
	detail TEXT,
	status TEXT,
	importance TEXT,
	recurrence TEXT,
	links TEXT,
	is_reminder_on TEXT,
	due_time DATETIME,
	completed_time DATETIME,
	last_modified DATETIME,
	sort INTEGER,
	sort_completed INTEGER,
	planer_sort INTEGER,
	sort_time INTEGER,
	planer_sort_time INTEGER,
	all_sort INTEGER,
	all_sort_completed INTEGER,
	all_sort_time INTEGER,
	recurrence_id TEXT,
	is_deleted TEXT NOT NULL DEFAULT 'N',
	updated_at DATETIME,
	FOREIGN KEY (user_id) REFERENCES users(id),
	FOREIGN KEY (task_list_id) REFERENCES schedule_groups(task_list_id)
);

-- Digests
CREATE TABLE IF NOT EXISTS summaries (
	id INTEGER PRIMARY KEY,
	user_id INTEGER NOT NULL,
	unique_identifier TEXT,
	name TEXT,
	description TEXT,
	file_id INTEGER,
	parent_unique_identifier TEXT,
	content TEXT,
	data_source TEXT,
	source_path TEXT,
	source_type TEXT,
	tags TEXT,
	md5_hash TEXT,
	handwrite_md5 TEXT,
	handwrite_inner_name TEXT,
	metadata TEXT,
	comment_fields TEXT,
	handwrite_fields TEXT,
	comment_handwrite_name TEXT,
	is_summary_group TEXT,
	author TEXT,
	creation_time DATETIME,
	last_modified_time DATETIME,
	updated_at DATETIME,
	FOREIGN KEY (user_id) REFERENCES users(id),
	FOREIGN KEY (file_id) REFERENCES files(id)
);

-- Notes Pipeline (from UltraBridge)
CREATE TABLE IF NOT EXISTS notes (
	path TEXT PRIMARY KEY,
	rel_path TEXT,
	file_type TEXT,
	size_bytes INTEGER,
	mtime DATETIME,
	sha256 TEXT,
	backup_path TEXT
);

CREATE TABLE IF NOT EXISTS jobs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	note_path TEXT NOT NULL UNIQUE,
	status TEXT,
	skip_reason TEXT,
	ocr_source TEXT,
	attempts INTEGER DEFAULT 0,
	last_error TEXT,
	requeue_after DATETIME,
	created_at DATETIME,
	updated_at DATETIME,
	started_at DATETIME,
	finished_at DATETIME,
	FOREIGN KEY (note_path) REFERENCES notes(path)
);

CREATE TABLE IF NOT EXISTS note_content (
	note_path TEXT NOT NULL,
	page INTEGER,
	title_text TEXT,
	body_text TEXT,
	keywords TEXT,
	source TEXT,
	PRIMARY KEY (note_path, page),
	FOREIGN KEY (note_path) REFERENCES notes(path)
);

-- FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS note_fts USING fts5(
	title_text,
	body_text,
	keywords,
	content=note_content,
	content_rowid='rowid'
);

-- FTS5 triggers for synchronization
CREATE TRIGGER IF NOT EXISTS note_content_ai AFTER INSERT ON note_content BEGIN
	INSERT INTO note_fts (rowid, title_text, body_text, keywords)
	VALUES (NEW.rowid, NEW.title_text, NEW.body_text, NEW.keywords);
END;

CREATE TRIGGER IF NOT EXISTS note_content_ad AFTER DELETE ON note_content BEGIN
	DELETE FROM note_fts WHERE rowid = OLD.rowid;
END;

CREATE TRIGGER IF NOT EXISTS note_content_au AFTER UPDATE ON note_content BEGIN
	UPDATE note_fts SET title_text=NEW.title_text, body_text=NEW.body_text, keywords=NEW.keywords
	WHERE rowid = NEW.rowid;
END;

-- Indexes matching opennotecloud
CREATE INDEX IF NOT EXISTS idx_files_user_dir ON files(user_id, directory_id);
CREATE INDEX IF NOT EXISTS idx_summaries_user ON summaries(user_id, is_summary_group);
CREATE INDEX IF NOT EXISTS idx_schedule_tasks_user ON schedule_tasks(user_id, updated_at);
CREATE INDEX IF NOT EXISTS idx_schedule_groups_user ON schedule_groups(user_id);
`

	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Fix NULL values in existing databases (schema defaults only apply to new rows)
	migrations := `
UPDATE files SET directory_id = 0 WHERE directory_id IS NULL;
UPDATE files SET inner_name = '' WHERE inner_name IS NULL;
UPDATE files SET storage_key = '' WHERE storage_key IS NULL;
UPDATE files SET md5 = '' WHERE md5 IS NULL;
UPDATE files SET size = 0 WHERE size IS NULL;
UPDATE files SET is_folder = 'N' WHERE is_folder IS NULL;
UPDATE files SET is_active = 'Y' WHERE is_active IS NULL;
UPDATE files SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL;
UPDATE files SET updated_at = CURRENT_TIMESTAMP WHERE updated_at IS NULL;
UPDATE recycle_files SET directory_id = 0 WHERE directory_id IS NULL;
UPDATE recycle_files SET inner_name = '' WHERE inner_name IS NULL;
UPDATE recycle_files SET storage_key = '' WHERE storage_key IS NULL;
UPDATE recycle_files SET md5 = '' WHERE md5 IS NULL;
UPDATE recycle_files SET size = 0 WHERE size IS NULL;
UPDATE recycle_files SET is_folder = 'N' WHERE is_folder IS NULL;
UPDATE recycle_files SET is_active = 'Y' WHERE is_active IS NULL;
UPDATE recycle_files SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL;
UPDATE recycle_files SET updated_at = CURRENT_TIMESTAMP WHERE updated_at IS NULL;
`
	_, err = db.Exec(migrations)
	return err
}
