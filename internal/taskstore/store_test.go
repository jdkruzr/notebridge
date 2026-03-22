package taskstore

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/sysop/notebridge/internal/syncdb"
)

func TestStoreList(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Initially list should be empty
	tasks, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("Expected empty list, got %d tasks", len(tasks))
	}

	// Create a task
	task := &Task{
		TaskID:     "test-1",
		Title:      SqlStr("Task 1"),
		Status:     SqlStr("needsAction"),
		UserID:     userID,
		TaskListID: SqlStr("default-list"),
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// List should now have one task
	tasks, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}
	if tasks[0].TaskID != "test-1" {
		t.Errorf("Expected task ID 'test-1', got %s", tasks[0].TaskID)
	}
}

func TestStoreGet(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Get non-existent task
	_, err := store.Get(ctx, "nonexistent")
	if !IsNotFound(err) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}

	// Create and retrieve a task
	task := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID: "test-2",
		Title:  SqlStr("Test Task"),
		Status: SqlStr("needsAction"),
		UserID: userID,
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.Get(ctx, "test-2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.TaskID != "test-2" {
		t.Errorf("Task ID mismatch: got %s", retrieved.TaskID)
	}
	if NullStr(retrieved.Title) != "Test Task" {
		t.Errorf("Title mismatch: got %s", NullStr(retrieved.Title))
	}
}

func TestStoreCreate(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	task := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID:  "create-test",
		Title:   SqlStr("Created Task"),
		Status:  SqlStr("needsAction"),
		UserID:  userID,
	}

	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it was created
	retrieved, err := store.Get(ctx, "create-test")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if NullStr(retrieved.Title) != "Created Task" {
		t.Errorf("Title mismatch after create")
	}
	if NullStr(retrieved.Status) != "needsAction" {
		t.Errorf("Status should default to needsAction")
	}
	if retrieved.IsDeleted != "N" {
		t.Errorf("IsDeleted should be 'N', got %s", retrieved.IsDeleted)
	}
	if !retrieved.LastModified.Valid {
		t.Errorf("LastModified should be set")
	}
	if !retrieved.CompletedTime.Valid {
		t.Errorf("CompletedTime should be set")
	}
}

func TestStoreUpdate(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Create a task
	task := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID:  "update-test",
		Title:   SqlStr("Original Title"),
		Status:  SqlStr("needsAction"),
		UserID:  userID,
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Get original LastModified
	original, _ := store.Get(ctx, "update-test")
	originalLM := original.LastModified.Int64

	// Update the task
	time.Sleep(1 * time.Millisecond) // Ensure time difference
	task.Title = SqlStr("Updated Title")
	task.Status = SqlStr("completed")
	task.TaskID = "update-test"
	task.UserID = userID

	if err := store.Update(ctx, task); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	updated, _ := store.Get(ctx, "update-test")
	if NullStr(updated.Title) != "Updated Title" {
		t.Errorf("Title not updated")
	}
	if NullStr(updated.Status) != "completed" {
		t.Errorf("Status not updated")
	}
	if updated.LastModified.Int64 <= originalLM {
		t.Errorf("LastModified should be updated")
	}
}

func TestStoreDelete(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Create a task
	task := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID: "delete-test",
		Title:  SqlStr("To Delete"),
		Status: SqlStr("needsAction"),
		UserID: userID,
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Delete it
	if err := store.Delete(ctx, "delete-test"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify soft delete (not found in List, but exists in DB with is_deleted='Y')
	_, err := store.Get(ctx, "delete-test")
	if !IsNotFound(err) {
		t.Errorf("Get should return ErrNotFound after delete")
	}

	// Verify is_deleted is 'Y' in DB
	row := db.QueryRowContext(ctx,
		"SELECT is_deleted FROM schedule_tasks WHERE task_id = ? AND user_id = ?",
		"delete-test", userID)
	var isDeleted string
	if err := row.Scan(&isDeleted); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if isDeleted != "Y" {
		t.Errorf("is_deleted should be 'Y', got %s", isDeleted)
	}
}

func TestStoreMaxLastModified(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Initially should be 0
	max, err := store.MaxLastModified(ctx)
	if err != nil {
		t.Fatalf("MaxLastModified failed: %v", err)
	}
	if max != 0 {
		t.Errorf("Expected 0, got %d", max)
	}

	// Create tasks with different LastModified values
	now := time.Now().UnixMilli()
	task1 := &Task{
		TaskID:       "task-1",
		TaskListID:   SqlStr("default-list"),
		Title:        SqlStr("Task 1"),
		Status:       SqlStr("needsAction"),
		UserID:       userID,
		LastModified: sql.NullInt64{Int64: now, Valid: true},
	}
	if err := store.Create(ctx, task1); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	time.Sleep(1 * time.Millisecond)

	task2 := &Task{
		TaskID:       "task-2",
		TaskListID:   SqlStr("default-list"),
		Title:        SqlStr("Task 2"),
		Status:       SqlStr("needsAction"),
		UserID:       userID,
		LastModified: sql.NullInt64{Int64: now + 10, Valid: true},
	}
	if err := store.Create(ctx, task2); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	max, err = store.MaxLastModified(ctx)
	if err != nil {
		t.Fatalf("MaxLastModified failed: %v", err)
	}
	if max <= now {
		t.Errorf("MaxLastModified should be greater than initial value")
	}

	// Delete task2 and verify MaxLastModified still reflects it
	if err := store.Delete(ctx, "task-2"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	max2, err := store.MaxLastModified(ctx)
	if err != nil {
		t.Fatalf("MaxLastModified failed: %v", err)
	}
	// After deletion, max should reflect remaining tasks
	// task2 had the highest value, so max should decrease
	if max2 >= max {
		t.Errorf("MaxLastModified should be less after deleting highest value task")
	}
}

func TestStoreETagComputation(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Create a task
	task := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID: "etag-test",
		Title:  SqlStr("Test Task"),
		Status: SqlStr("needsAction"),
		UserID: userID,
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, _ := store.Get(ctx, "etag-test")
	etag1 := ComputeETag(retrieved)

	// Update the task
	time.Sleep(1 * time.Millisecond)
	retrieved.Title = SqlStr("Updated Title")
	if err := store.Update(ctx, retrieved); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	updated, _ := store.Get(ctx, "etag-test")
	etag2 := ComputeETag(updated)

	if etag1 == etag2 {
		t.Errorf("ETag should change when task is updated")
	}
}

func TestStoreCTagComputation(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := New(db, userID)
	ctx := context.Background()

	// Create task 1
	task1 := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID: "ctag-1",
		Title:  SqlStr("Task 1"),
		Status: SqlStr("needsAction"),
		UserID: userID,
	}
	if err := store.Create(ctx, task1); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tasks, _ := store.List(ctx)
	ctag1 := ComputeCTag(tasks)

	// Create task 2
	time.Sleep(1 * time.Millisecond)
	task2 := &Task{
		TaskListID: SqlStr("default-list"),
		TaskID: "ctag-2",
		Title:  SqlStr("Task 2"),
		Status: SqlStr("needsAction"),
		UserID: userID,
	}
	if err := store.Create(ctx, task2); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tasks, _ = store.List(ctx)
	ctag2 := ComputeCTag(tasks)

	if ctag1 == ctag2 {
		t.Errorf("CTag should change when a new task is created")
	}
}

// Helper function to set up a test database
func setupTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()

	db, err := syncdb.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}

	// Insert a test user into the users table (created by syncdb schema)
	result, err := db.Exec(
		"INSERT INTO users (email, password_hash, username) VALUES (?, ?, ?)",
		"testuser@example.com",
		"$2a$10$...",
		"testuser",
	)
	if err != nil {
		t.Fatalf("Failed to insert test user: %v", err)
	}

	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("Failed to get user ID: %v", err)
	}

	// Create a task list group (required by schedule_tasks foreign key)
	_, err = db.Exec(
		"INSERT INTO schedule_groups (task_list_id, user_id, title) VALUES (?, ?, ?)",
		"default-list",
		userID,
		"Default Tasks",
	)
	if err != nil {
		t.Fatalf("Failed to create task list: %v", err)
	}

	return db, userID
}
