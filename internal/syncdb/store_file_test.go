package syncdb

import (
	"context"
	"testing"
	"time"
)

// setupTestStore creates an in-memory SQLite database with schema for testing.
func setupTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	store := NewStore(db)

	// Create a test user
	ctx := context.Background()
	err = store.EnsureUser(ctx, "test@example.com", "testhash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	return store
}

// === Sync Lock Tests ===

func TestAcquireLockSuccess(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Get the test user ID
	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Acquire lock
	err = store.AcquireLock(ctx, user.ID, "device1")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}
}

// AC2.7: Second device should get ErrSyncLocked
func TestAcquireLockConflict(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Device A acquires lock
	err = store.AcquireLock(ctx, user.ID, "device-A")
	if err != nil {
		t.Fatalf("failed to acquire lock for device A: %v", err)
	}

	// Device B tries to acquire same lock
	err = store.AcquireLock(ctx, user.ID, "device-B")
	if err != ErrSyncLocked {
		t.Errorf("expected ErrSyncLocked, got %v", err)
	}
}

// AC2.10: Expired lock should allow another device to acquire
func TestAcquireLockAfterExpiry(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Device A acquires lock
	err = store.AcquireLock(ctx, user.ID, "device-A")
	if err != nil {
		t.Fatalf("failed to acquire lock for device A: %v", err)
	}

	// Manually expire the lock in DB by setting expires_at to past
	expiredTime := time.Now().Add(-1 * time.Minute).Format(time.RFC3339Nano)
	_, err = store.db.ExecContext(ctx, "UPDATE sync_locks SET expires_at = ? WHERE user_id = ?", expiredTime, user.ID)
	if err != nil {
		t.Fatalf("failed to expire lock: %v", err)
	}

	// Device B should now be able to acquire lock
	err = store.AcquireLock(ctx, user.ID, "device-B")
	if err != nil {
		t.Errorf("failed to acquire lock after expiry: %v", err)
	}
}

func TestReleaseLock(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Acquire lock
	err = store.AcquireLock(ctx, user.ID, "device1")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Release lock
	err = store.ReleaseLock(ctx, user.ID, "device1")
	if err != nil {
		t.Fatalf("failed to release lock: %v", err)
	}

	// Verify another device can now acquire
	err = store.AcquireLock(ctx, user.ID, "device2")
	if err != nil {
		t.Errorf("failed to acquire lock after release: %v", err)
	}
}

func TestRefreshLock(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Acquire lock
	err = store.AcquireLock(ctx, user.ID, "device1")
	if err != nil {
		t.Fatalf("failed to acquire lock: %v", err)
	}

	// Refresh lock
	err = store.RefreshLock(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to refresh lock: %v", err)
	}

	// Verify lock still exists and is unexpired
	now := time.Now().Format(time.RFC3339Nano)
	var count int
	err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_locks WHERE user_id = ? AND expires_at > ?", user.ID, now).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query lock: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 lock, got %d", count)
	}
}

// === File Catalog Tests ===

func TestCreateFileAndGetFile(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          1001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "test.txt",
		InnerName:   "test.txt",
		StorageKey:  "user@example.com/test.txt",
		MD5:         "abc123",
		Size:        1024,
		IsFolder:    false,
		IsActive:    true,
	}

	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Retrieve the file
	retrieved, err := store.GetFile(ctx, 1001, user.ID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}

	if retrieved == nil {
		t.Fatal("file not found")
	}

	if retrieved.FileName != "test.txt" {
		t.Errorf("expected filename test.txt, got %s", retrieved.FileName)
	}

	if retrieved.MD5 != "abc123" {
		t.Errorf("expected md5 abc123, got %s", retrieved.MD5)
	}

	if retrieved.Size != 1024 {
		t.Errorf("expected size 1024, got %d", retrieved.Size)
	}
}

func TestGetFileByPath(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          1002,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "notes.pdf",
		InnerName:   "notes.pdf",
		StorageKey:  "user@example.com/notes.pdf",
		MD5:         "def456",
		Size:        2048,
		IsFolder:    false,
		IsActive:    true,
	}

	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Retrieve by path
	retrieved, err := store.GetFileByPath(ctx, user.ID, 0, "notes.pdf")
	if err != nil {
		t.Fatalf("failed to get file by path: %v", err)
	}

	if retrieved == nil {
		t.Fatal("file not found")
	}

	if retrieved.FileName != "notes.pdf" {
		t.Errorf("expected filename notes.pdf, got %s", retrieved.FileName)
	}
}

func TestUpdateFileMD5(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          1003,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "data.bin",
		InnerName:   "data.bin",
		StorageKey:  "user@example.com/data.bin",
		MD5:         "oldmd5",
		Size:        100,
		IsFolder:    false,
		IsActive:    true,
	}

	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Update MD5 and size
	err = store.UpdateFileMD5(ctx, 1003, "newmd5", 200)
	if err != nil {
		t.Fatalf("failed to update md5: %v", err)
	}

	// Retrieve and verify
	updated, err := store.GetFile(ctx, 1003, user.ID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}

	if updated.MD5 != "newmd5" {
		t.Errorf("expected md5 newmd5, got %s", updated.MD5)
	}

	if updated.Size != 200 {
		t.Errorf("expected size 200, got %d", updated.Size)
	}
}

func TestListFolder(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a folder
	folder := &FileEntry{
		ID:          2001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "Folder1",
		InnerName:   "Folder1",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder)
	if err != nil {
		t.Fatalf("failed to create folder: %v", err)
	}

	// Create files in root
	file1 := &FileEntry{
		ID:          2002,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "aaa.txt",
		InnerName:   "aaa.txt",
		StorageKey:  "user@example.com/aaa.txt",
		MD5:         "aaa",
		Size:        100,
		IsFolder:    false,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, file1)
	if err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}

	file2 := &FileEntry{
		ID:          2003,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "zzz.txt",
		InnerName:   "zzz.txt",
		StorageKey:  "user@example.com/zzz.txt",
		MD5:         "zzz",
		Size:        200,
		IsFolder:    false,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, file2)
	if err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	// List folder - should have folders first, then files by name
	entries, err := store.ListFolder(ctx, user.ID, 0)
	if err != nil {
		t.Fatalf("failed to list folder: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// First should be folder
	if !entries[0].IsFolder {
		t.Errorf("expected first entry to be folder, got file %s", entries[0].FileName)
	}

	// Files should be sorted by name (aaa before zzz)
	if entries[1].FileName != "aaa.txt" {
		t.Errorf("expected second entry to be aaa.txt, got %s", entries[1].FileName)
	}

	if entries[2].FileName != "zzz.txt" {
		t.Errorf("expected third entry to be zzz.txt, got %s", entries[2].FileName)
	}
}

func TestListFolderRecursive(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create nested structure:
	// root
	//   ├─ folder1
	//   │   └─ file1.txt
	//   └─ file0.txt

	folder1 := &FileEntry{
		ID:          3001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "folder1",
		InnerName:   "folder1",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder1)
	if err != nil {
		t.Fatalf("failed to create folder1: %v", err)
	}

	file0 := &FileEntry{
		ID:          3002,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "file0.txt",
		InnerName:   "file0.txt",
		StorageKey:  "user@example.com/file0.txt",
		MD5:         "file0",
		Size:        100,
		IsFolder:    false,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, file0)
	if err != nil {
		t.Fatalf("failed to create file0: %v", err)
	}

	file1 := &FileEntry{
		ID:          3003,
		UserID:      user.ID,
		DirectoryID: 3001,
		FileName:    "file1.txt",
		InnerName:   "file1.txt",
		StorageKey:  "user@example.com/folder1/file1.txt",
		MD5:         "file1",
		Size:        200,
		IsFolder:    false,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, file1)
	if err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}

	// List recursively
	entries, err := store.ListFolderRecursive(ctx, user.ID, 0)
	if err != nil {
		t.Fatalf("failed to list recursively: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Should include folder1, file0.txt, and file1.txt
	fileNames := make(map[string]bool)
	for _, e := range entries {
		fileNames[e.FileName] = true
	}

	if !fileNames["folder1"] {
		t.Error("folder1 not found in recursive listing")
	}

	if !fileNames["file0.txt"] {
		t.Error("file0.txt not found in recursive listing")
	}

	if !fileNames["file1.txt"] {
		t.Error("file1.txt not found in recursive listing")
	}
}

// AC2.4: Soft delete
func TestSoftDelete(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          4001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "delete_me.txt",
		InnerName:   "delete_me.txt",
		StorageKey:  "user@example.com/delete_me.txt",
		MD5:         "delete",
		Size:        500,
		IsFolder:    false,
		IsActive:    true,
	}

	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Soft delete
	err = store.SoftDelete(ctx, 4001, user.ID)
	if err != nil {
		t.Fatalf("failed to soft delete: %v", err)
	}

	// Verify file no longer in files table
	retrieved, err := store.GetFile(ctx, 4001, user.ID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}

	if retrieved != nil {
		t.Error("file should not exist in files table after soft delete")
	}

	// Verify file is in recycle_files table
	var count int
	err = store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM recycle_files WHERE id = ? AND user_id = ?", 4001, user.ID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query recycle_files: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 file in recycle_files, got %d", count)
	}
}

func TestSoftDeleteNotInList(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          4002,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "test_file.txt",
		InnerName:   "test_file.txt",
		StorageKey:  "user@example.com/test_file.txt",
		MD5:         "test",
		Size:        600,
		IsFolder:    false,
		IsActive:    true,
	}

	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Soft delete
	err = store.SoftDelete(ctx, 4002, user.ID)
	if err != nil {
		t.Fatalf("failed to soft delete: %v", err)
	}

	// List folder - file should not appear
	entries, err := store.ListFolder(ctx, user.ID, 0)
	if err != nil {
		t.Fatalf("failed to list folder: %v", err)
	}

	for _, e := range entries {
		if e.ID == 4002 {
			t.Error("deleted file still appears in ListFolder")
		}
	}
}

func TestMoveFile(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create a folder
	folder := &FileEntry{
		ID:          5001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "MyFolder",
		InnerName:   "MyFolder",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder)
	if err != nil {
		t.Fatalf("failed to create folder: %v", err)
	}

	// Create a file
	file := &FileEntry{
		ID:          5002,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "move_me.txt",
		InnerName:   "move_me.txt",
		StorageKey:  "user@example.com/move_me.txt",
		MD5:         "move",
		Size:        300,
		IsFolder:    false,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, file)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Move file to folder
	err = store.MoveFile(ctx, 5002, 5001, "moved_file.txt")
	if err != nil {
		t.Fatalf("failed to move file: %v", err)
	}

	// Verify file was moved
	updated, err := store.GetFile(ctx, 5002, user.ID)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}

	if updated.DirectoryID != 5001 {
		t.Errorf("expected directory_id 5001, got %d", updated.DirectoryID)
	}

	if updated.FileName != "moved_file.txt" {
		t.Errorf("expected filename moved_file.txt, got %s", updated.FileName)
	}
}

func TestGetAncestorIDs(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create nested folders: root -> folder1 -> folder2 -> folder3
	folder1 := &FileEntry{
		ID:          6001,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "folder1",
		InnerName:   "folder1",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder1)
	if err != nil {
		t.Fatalf("failed to create folder1: %v", err)
	}

	folder2 := &FileEntry{
		ID:          6002,
		UserID:      user.ID,
		DirectoryID: 6001,
		FileName:    "folder2",
		InnerName:   "folder2",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder2)
	if err != nil {
		t.Fatalf("failed to create folder2: %v", err)
	}

	folder3 := &FileEntry{
		ID:          6003,
		UserID:      user.ID,
		DirectoryID: 6002,
		FileName:    "folder3",
		InnerName:   "folder3",
		StorageKey:  "",
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder3)
	if err != nil {
		t.Fatalf("failed to create folder3: %v", err)
	}

	// Get ancestors of folder3
	ancestors, err := store.GetAncestorIDs(ctx, 6003, 100)
	if err != nil {
		t.Fatalf("failed to get ancestors: %v", err)
	}

	// Should be [6003, 6002, 6001, 0]
	if len(ancestors) < 3 {
		t.Fatalf("expected at least 3 ancestors, got %d", len(ancestors))
	}

	if ancestors[0] != 6003 {
		t.Errorf("expected first ancestor 6003, got %d", ancestors[0])
	}

	if ancestors[1] != 6002 {
		t.Errorf("expected second ancestor 6002, got %d", ancestors[1])
	}

	if ancestors[2] != 6001 {
		t.Errorf("expected third ancestor 6001, got %d", ancestors[2])
	}
}

func TestFindByName(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create files with similar names
	files := []string{"notes.txt", "notes(1).txt", "notes(2).txt", "other.txt"}
	for i, name := range files {
		f := &FileEntry{
			ID:          int64(7000 + i),
			UserID:      user.ID,
			DirectoryID: 0,
			FileName:    name,
			InnerName:   name,
			StorageKey:  "user@example.com/" + name,
			MD5:         "md5" + string(rune(i)),
			Size:        100,
			IsFolder:    false,
			IsActive:    true,
		}
		err = store.CreateFile(ctx, f)
		if err != nil {
			t.Fatalf("failed to create file %s: %v", name, err)
		}
	}

	// Find names matching "notes"
	matches, err := store.FindByName(ctx, user.ID, 0, "notes")
	if err != nil {
		t.Fatalf("failed to find by name: %v", err)
	}

	if len(matches) != 3 {
		t.Fatalf("expected 3 matches for 'notes', got %d: %v", len(matches), matches)
	}
}

func TestSpaceUsage(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create some files with various sizes
	sizes := []int64{100, 200, 300}
	for i, size := range sizes {
		f := &FileEntry{
			ID:          int64(8000 + i),
			UserID:      user.ID,
			DirectoryID: 0,
			FileName:    "file" + string(rune('0'+i)),
			InnerName:   "file" + string(rune('0'+i)),
			StorageKey:  "user@example.com/file" + string(rune('0'+i)),
			MD5:         "md5",
			Size:        size,
			IsFolder:    false,
			IsActive:    true,
		}
		err = store.CreateFile(ctx, f)
		if err != nil {
			t.Fatalf("failed to create file: %v", err)
		}
	}

	// Create a folder (shouldn't count)
	folder := &FileEntry{
		ID:          8099,
		UserID:      user.ID,
		DirectoryID: 0,
		FileName:    "folder",
		InnerName:   "folder",
		StorageKey:  "",
		Size:        999, // Folders shouldn't count
		IsFolder:    true,
		IsActive:    true,
	}
	err = store.CreateFile(ctx, folder)
	if err != nil {
		t.Fatalf("failed to create folder: %v", err)
	}

	// Get space usage
	total, err := store.SpaceUsage(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to get space usage: %v", err)
	}

	expected := int64(600) // 100 + 200 + 300
	if total != expected {
		t.Errorf("expected total size %d, got %d", expected, total)
	}
}

// === Chunk Tracking Tests ===

func TestSaveChunkRecordAndCount(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	uploadID := "upload-123"

	// Save chunk records
	err := store.SaveChunkRecord(ctx, uploadID, 1, 3, "md5part1", "/path/part1")
	if err != nil {
		t.Fatalf("failed to save chunk 1: %v", err)
	}

	err = store.SaveChunkRecord(ctx, uploadID, 2, 3, "md5part2", "/path/part2")
	if err != nil {
		t.Fatalf("failed to save chunk 2: %v", err)
	}

	err = store.SaveChunkRecord(ctx, uploadID, 3, 3, "md5part3", "/path/part3")
	if err != nil {
		t.Fatalf("failed to save chunk 3: %v", err)
	}

	// Count chunks
	count, err := store.CountChunks(ctx, uploadID)
	if err != nil {
		t.Fatalf("failed to count chunks: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 chunks, got %d", count)
	}
}

func TestDeleteChunkRecords(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	uploadID := "upload-456"

	// Save chunk records
	err := store.SaveChunkRecord(ctx, uploadID, 1, 2, "md5a", "/path/a")
	if err != nil {
		t.Fatalf("failed to save chunk: %v", err)
	}

	err = store.SaveChunkRecord(ctx, uploadID, 2, 2, "md5b", "/path/b")
	if err != nil {
		t.Fatalf("failed to save chunk: %v", err)
	}

	// Delete chunks
	err = store.DeleteChunkRecords(ctx, uploadID)
	if err != nil {
		t.Fatalf("failed to delete chunks: %v", err)
	}

	// Verify deleted
	count, err := store.CountChunks(ctx, uploadID)
	if err != nil {
		t.Fatalf("failed to count chunks: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 chunks after delete, got %d", count)
	}
}
