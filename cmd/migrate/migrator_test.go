package main

import (
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/sync"
	"github.com/sysop/notebridge/internal/syncdb"
)

// mockSPCReader implements a mock SPC reader for testing.
type mockSPCReader struct {
	user         *SPCUser
	files        []SPCFile
	tasks        []SPCTask
	taskGroups   []SPCTaskGroup
	summaries    []SPCSummary
}

// Helper to create mock reader
func newMockSPCReader(user *SPCUser, files []SPCFile, tasks []SPCTask, taskGroups []SPCTaskGroup, summaries []SPCSummary) *mockSPCReader {
	return &mockSPCReader{
		user:       user,
		files:      files,
		tasks:      tasks,
		taskGroups: taskGroups,
		summaries:  summaries,
	}
}

func (m *mockSPCReader) ReadUser(ctx context.Context) (*SPCUser, error) {
	return m.user, nil
}

func (m *mockSPCReader) ReadFiles(ctx context.Context, userID int64) ([]SPCFile, error) {
	return m.files, nil
}

func (m *mockSPCReader) ReadTasks(ctx context.Context, userID int64) ([]SPCTask, error) {
	return m.tasks, nil
}

func (m *mockSPCReader) ReadTaskGroups(ctx context.Context, userID int64) ([]SPCTaskGroup, error) {
	return m.taskGroups, nil
}

func (m *mockSPCReader) ReadSummaries(ctx context.Context, userID int64) ([]SPCSummary, error) {
	return m.summaries, nil
}

func (m *mockSPCReader) Close() error {
	return nil
}

// TestHelper creates a test database
func createTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create schema
	schema := `
	CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		error_count INTEGER,
		last_error_at TEXT,
		locked_until TEXT
	);
	CREATE TABLE equipment (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		equipment_no TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		status TEXT,
		UNIQUE(equipment_no, user_id),
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	CREATE TABLE files (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		directory_id INTEGER,
		file_name TEXT NOT NULL,
		inner_name TEXT,
		storage_key TEXT,
		md5 TEXT,
		size INTEGER,
		is_folder TEXT,
		is_active TEXT,
		created_at TEXT,
		updated_at TEXT,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	CREATE TABLE schedule_groups (
		task_list_id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		last_modified INTEGER,
		create_time INTEGER,
		updated_at TEXT,
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	CREATE TABLE schedule_tasks (
		task_id TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		task_list_id TEXT,
		title TEXT NOT NULL,
		detail TEXT,
		status TEXT,
		importance TEXT,
		recurrence TEXT,
		links TEXT,
		is_reminder_on TEXT,
		due_time INTEGER,
		completed_time INTEGER,
		last_modified INTEGER,
		sort INTEGER,
		sort_completed INTEGER,
		planer_sort INTEGER,
		sort_time INTEGER,
		planer_sort_time INTEGER,
		all_sort INTEGER,
		all_sort_completed INTEGER,
		all_sort_time INTEGER,
		recurrence_id TEXT,
		updated_at TEXT,
		FOREIGN KEY(user_id) REFERENCES users(id),
		FOREIGN KEY(task_list_id) REFERENCES schedule_groups(task_list_id)
	);
	CREATE TABLE summaries (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		unique_identifier TEXT NOT NULL,
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
		creation_time INTEGER,
		last_modified_time INTEGER,
		updated_at TEXT,
		UNIQUE(user_id, unique_identifier),
		FOREIGN KEY(user_id) REFERENCES users(id)
	);
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}

	return db
}

// TestMigration_FileMigration tests basic file migration
func TestMigration_FileMigration(t *testing.T) {
	ctx := context.Background()

	// Setup
	tmpDir := t.TempDir()
	spcDataPath := tmpDir
	nbStoragePath := filepath.Join(tmpDir, "storage")

	// Write test file - path is spcPath/email/Supernote/folderPath/innerName
	testContent := []byte("test file content")
	testMD5 := fmt.Sprintf("%x", md5.Sum(testContent))
	testFileDir := filepath.Join(spcDataPath, "user@example.com", "Supernote", "Documents")
	os.MkdirAll(testFileDir, 0755)
	testFilePath := filepath.Join(testFileDir, "test.note")
	if err := os.WriteFile(testFilePath, testContent, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create mock data - FileName is the file's own name, InnerName is how it's stored on disk
	spcUser := &SPCUser{UserID: 1, Email: "user@example.com", PasswordHash: "abc123", Username: "testuser"}
	spcFiles := []SPCFile{
		{ID: 1, DirectoryID: 0, FileName: "Documents", InnerName: "", IsFolder: true, CreateTime: 0, UpdateTime: 0},
		{ID: 100, DirectoryID: 1, FileName: "test.note", InnerName: "test.note", MD5: testMD5, Size: int64(len(testContent)), IsFolder: false, CreateTime: 0, UpdateTime: 0},
	}

	mockReader := newMockSPCReader(spcUser, spcFiles, nil, nil, nil)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	blobStore := blob.NewLocalStore(nbStoragePath)
	sf := sync.NewSnowflakeGenerator()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: SPCReaderInterface(mockReader),
		syncStore: store,
		blobStore: blobStore,
		snowflake: sf,
		spcPath:   spcDataPath,
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}

	// Run migration
	if err := migrator.migrateUser(ctx); err != nil {
		t.Fatalf("failed to migrate user: %v", err)
	}
	if err := migrator.migrateFiles(ctx); err != nil {
		t.Fatalf("failed to migrate files: %v", err)
	}

	// Verify results
	if migrator.stats.FoldersMigrated != 1 {
		t.Errorf("expected 1 folder migrated, got %d", migrator.stats.FoldersMigrated)
	}
	if migrator.stats.FilesMigrated != 1 {
		t.Errorf("expected 1 file migrated, got %d", migrator.stats.FilesMigrated)
	}
	if migrator.stats.BytesMigrated != int64(len(testContent)) {
		t.Errorf("expected %d bytes migrated, got %d", len(testContent), migrator.stats.BytesMigrated)
	}

	// Verify file exists in storage
	entries, err := os.ReadDir(nbStoragePath)
	if err != nil {
		t.Fatalf("failed to read storage dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected files in storage directory")
	}
}

// TestMigration_MissingFileHandling tests that missing files are logged but don't stop migration
func TestMigration_MissingFileHandling(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	spcDataPath := filepath.Join(tmpDir, "spc")
	nbStoragePath := filepath.Join(tmpDir, "storage")

	os.MkdirAll(filepath.Join(spcDataPath, "user@example.com", "Supernote"), 0755)

	// Create mock data with missing files
	spcUser := &SPCUser{UserID: 1, Email: "user@example.com", PasswordHash: "abc123", Username: "testuser"}
	spcFiles := []SPCFile{
		{ID: 1, DirectoryID: 0, FileName: "Documents", InnerName: "", IsFolder: true},
		{ID: 100, DirectoryID: 1, FileName: "missing.note", InnerName: "missing.note", MD5: "xyz", Size: 100, IsFolder: false},
		{ID: 101, DirectoryID: 1, FileName: "also_missing.pdf", InnerName: "also_missing.pdf", MD5: "abc", Size: 200, IsFolder: false},
	}

	mockReader := newMockSPCReader(spcUser, spcFiles, nil, nil, nil)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	blobStore := blob.NewLocalStore(nbStoragePath)
	sf := sync.NewSnowflakeGenerator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: SPCReaderInterface(mockReader),
		syncStore: store,
		blobStore: blobStore,
		snowflake: sf,
		spcPath:   spcDataPath,
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}

	if err := migrator.migrateUser(ctx); err != nil {
		t.Fatalf("failed to migrate user: %v", err)
	}
	if err := migrator.migrateFiles(ctx); err != nil {
		t.Fatalf("failed to migrate files: %v", err)
	}

	// Verify migration continued despite missing files
	if migrator.stats.FoldersMigrated != 1 {
		t.Errorf("expected 1 folder, got %d", migrator.stats.FoldersMigrated)
	}
	if migrator.stats.FilesMigrated != 0 {
		t.Errorf("expected 0 files migrated (all missing), got %d", migrator.stats.FilesMigrated)
	}
	if migrator.stats.MissingFiles != 2 {
		t.Errorf("expected 2 missing files, got %d", migrator.stats.MissingFiles)
	}
}

// TestMigration_TaskMigration tests task and task group migration
func TestMigration_TaskMigration(t *testing.T) {
	ctx := context.Background()

	spcUser := &SPCUser{UserID: 1, Email: "user@example.com", PasswordHash: "abc123", Username: "testuser"}
	spcTaskGroups := []SPCTaskGroup{
		{TaskListID: "group1", Title: "My Tasks", LastModified: 1000},
		{TaskListID: "group2", Title: "Work", LastModified: 2000},
	}
	spcTasks := []SPCTask{
		{TaskID: "task1", TaskListID: "group1", Title: "Task 1", Status: "open", Importance: "high", DueTime: 1000, LastModified: 1000},
		{TaskID: "task2", TaskListID: "group1", Title: "Task 2", Status: "completed", Importance: "normal", DueTime: 2000, LastModified: 2000},
		{TaskID: "task3", TaskListID: "group2", Title: "Work Task", Status: "open", Importance: "medium", DueTime: 3000, LastModified: 3000},
	}

	mockReader := newMockSPCReader(spcUser, nil, spcTasks, spcTaskGroups, nil)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	sf := sync.NewSnowflakeGenerator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: mockReader,
		syncStore: store,
		blobStore: nil,
		snowflake: sf,
		spcPath:   "",
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}

	if err := migrator.migrateUser(ctx); err != nil {
		t.Fatalf("failed to migrate user: %v", err)
	}
	if err := migrator.migrateTaskGroups(ctx); err != nil {
		t.Fatalf("failed to migrate task groups: %v", err)
	}
	if err := migrator.migrateTasks(ctx); err != nil {
		t.Fatalf("failed to migrate tasks: %v", err)
	}

	if migrator.stats.TaskGroupsMigrated != 2 {
		t.Errorf("expected 2 task groups, got %d", migrator.stats.TaskGroupsMigrated)
	}
	if migrator.stats.TasksMigrated != 3 {
		t.Errorf("expected 3 tasks, got %d", migrator.stats.TasksMigrated)
	}

	// Verify task fields
	user, _ := store.GetUserByEmail(ctx, "user@example.com")
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks in DB, got %d", len(tasks))
	}

	// Find task1 and verify fields
	for _, task := range tasks {
		if task.TaskID == "task1" {
			if task.Title != "Task 1" {
				t.Errorf("expected title 'Task 1', got %s", task.Title)
			}
			if task.Status != "open" {
				t.Errorf("expected status 'open', got %s", task.Status)
			}
			if task.Importance != "high" {
				t.Errorf("expected importance 'high', got %s", task.Importance)
			}
		}
	}
}

// TestMigration_SummaryMigration tests summary migration
func TestMigration_SummaryMigration(t *testing.T) {
	ctx := context.Background()

	spcUser := &SPCUser{UserID: 1, Email: "user@example.com", PasswordHash: "abc123", Username: "testuser"}
	spcSummaries := []SPCSummary{
		{ID: 1, UniqueIdentifier: "summary1", Name: "Summary 1", IsSummaryGroup: true, CreationTime: 1000, LastModifiedTime: 1000},
		{ID: 2, UniqueIdentifier: "item1", Name: "Item 1", ParentUniqueIdentifier: "summary1", IsSummaryGroup: false, CreationTime: 1000, LastModifiedTime: 1000},
		{ID: 3, UniqueIdentifier: "item2", Name: "Item 2", ParentUniqueIdentifier: "summary1", IsSummaryGroup: false, CreationTime: 1000, LastModifiedTime: 1000},
	}

	mockReader := newMockSPCReader(spcUser, nil, nil, nil, spcSummaries)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	sf := sync.NewSnowflakeGenerator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: mockReader,
		syncStore: store,
		blobStore: blob.NewLocalStore(t.TempDir()),
		snowflake: sf,
		spcPath:   "",
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}

	if err := migrator.migrateUser(ctx); err != nil {
		t.Fatalf("failed to migrate user: %v", err)
	}
	if err := migrator.migrateSummaries(ctx); err != nil {
		t.Fatalf("failed to migrate summaries: %v", err)
	}

	if migrator.stats.SummariesMigrated != 3 {
		t.Errorf("expected 3 summaries, got %d", migrator.stats.SummariesMigrated)
	}

	// Verify summaries in DB
	user, _ := store.GetUserByEmail(ctx, "user@example.com")
	groups, _, err := store.ListSummaryGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list summary groups: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("expected 1 summary group, got %d", len(groups))
	}
}

// TestMigration_UserMigration tests user and equipment creation
func TestMigration_UserMigration(t *testing.T) {
	ctx := context.Background()

	spcUser := &SPCUser{
		UserID:       1,
		Email:        "testuser@example.com",
		PasswordHash: "abc123def456",
		Username:     "testuser",
	}

	mockReader := newMockSPCReader(spcUser, nil, nil, nil, nil)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	sf := sync.NewSnowflakeGenerator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: SPCReaderInterface(mockReader),
		syncStore: store,
		snowflake: sf,
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}

	if err := migrator.migrateUser(ctx); err != nil {
		t.Fatalf("failed to migrate user: %v", err)
	}

	if migrator.stats.UsersMigrated != 1 {
		t.Errorf("expected 1 user migrated, got %d", migrator.stats.UsersMigrated)
	}

	// Verify user in DB
	user, err := store.GetUserByEmail(ctx, "testuser@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if user == nil {
		t.Fatal("expected user to exist")
	}
	if user.Email != "testuser@example.com" {
		t.Errorf("expected email 'testuser@example.com', got %s", user.Email)
	}
	if user.PasswordHash != "abc123def456" {
		t.Errorf("expected password hash 'abc123def456', got %s", user.PasswordHash)
	}
}

// TestMigration_DryRun tests that dry run doesn't write anything
func TestMigration_DryRun(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	spcDataPath := tmpDir
	nbStoragePath := filepath.Join(tmpDir, "storage")

	testFileDir := filepath.Join(spcDataPath, "user@example.com", "Supernote", "Documents")
	os.MkdirAll(testFileDir, 0755)
	testContent := []byte("test content")
	testMD5 := fmt.Sprintf("%x", md5.Sum(testContent))
	testFilePath := filepath.Join(testFileDir, "test.note")
	os.WriteFile(testFilePath, testContent, 0644)

	spcUser := &SPCUser{UserID: 1, Email: "user@example.com", PasswordHash: "abc123", Username: "testuser"}
	spcFiles := []SPCFile{
		{ID: 1, DirectoryID: 0, FileName: "Documents", InnerName: "", IsFolder: true},
		{ID: 100, DirectoryID: 1, FileName: "test.note", InnerName: "test.note", MD5: testMD5, Size: int64(len(testContent)), IsFolder: false},
	}
	spcTasks := []SPCTask{
		{TaskID: "task1", TaskListID: "group1", Title: "Task 1", Status: "open"},
	}
	spcTaskGroups := []SPCTaskGroup{
		{TaskListID: "group1", Title: "My Tasks", LastModified: 1000},
	}

	mockReader := newMockSPCReader(spcUser, spcFiles, spcTasks, spcTaskGroups, nil)
	db := createTestDB(t)
	defer db.Close()

	store := syncdb.NewStore(db)
	blobStore := blob.NewLocalStore(nbStoragePath)
	sf := sync.NewSnowflakeGenerator()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	migrator := &Migrator{
		spcReader: mockReader,
		syncStore: store,
		blobStore: blobStore,
		snowflake: sf,
		spcPath:   spcDataPath,
		logger:    logger,
		dryRun:    true,
		dirMap:    make(map[int64]int64),
	}

	if err := migrator.Run(ctx); err != nil {
		t.Fatalf("dry run failed: %v", err)
	}

	// Verify nothing was written to DB
	user, err := store.GetUserByEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Error("expected no user to be written in dry run")
	}

	// Verify stats show what would be migrated
	if migrator.stats.UsersMigrated != 1 {
		t.Errorf("expected stats to show 1 user, got %d", migrator.stats.UsersMigrated)
	}
	if migrator.stats.FilesMigrated != 1 {
		t.Errorf("expected stats to show 1 file, got %d", migrator.stats.FilesMigrated)
	}
}
