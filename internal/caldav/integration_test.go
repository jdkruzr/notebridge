package caldav

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/sysop/notebridge/internal/events"
	"github.com/sysop/notebridge/internal/syncdb"
	"github.com/sysop/notebridge/internal/taskstore"
)

// TestIntegration_AC7_1_TabletTasksAppearInCalDAV verifies AC7.1:
// Tasks synced from tablet appear as VTODOs via CalDAV
func TestIntegration_AC7_1_TabletTasksAppearInCalDAV(t *testing.T) {
	// Setup: in-memory database with CalDAV backend
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	backend := NewBackend(store, "/caldav", "Supernote Tasks", "preserve", nil)
	ctx := context.Background()

	// Simulate tablet sync: create a task via store (as if from device sync API)
	task := &taskstore.Task{
		TaskID:       "tablet-task-1",
		TaskListID:   taskstore.SqlStr("default-list"),
		Title:        taskstore.SqlStr("Buy Groceries"),
		Status:       taskstore.SqlStr("needsAction"),
		DueTime:      taskstore.TimeToMs(time.Date(2025, 3, 25, 10, 0, 0, 0, time.UTC)),
		LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Failed to create task: %v", err)
	}

	// AC7.1: Verify task appears in CalDAV list
	objects, err := backend.ListCalendarObjects(ctx, "/caldav/user/calendars/tasks/", nil)
	if err != nil {
		t.Fatalf("ListCalendarObjects failed: %v", err)
	}

	if len(objects) != 1 {
		t.Errorf("Expected 1 CalDAV object, got %d", len(objects))
	}

	// Verify VTODO SUMMARY matches task title
	obj := objects[0]
	if obj.Path != "/caldav/user/calendars/tasks/tablet-task-1.ics" {
		t.Errorf("Path mismatch: got %q", obj.Path)
	}

	todo, err := FindVTODO(obj.Data)
	if err != nil {
		t.Fatalf("FindVTODO failed: %v", err)
	}

	if todo.Props.Get("SUMMARY").Value != "Buy Groceries" {
		t.Errorf("SUMMARY mismatch: got %q", todo.Props.Get("SUMMARY").Value)
	}

	// Verify VTODO STATUS matches task status
	if todo.Props.Get("STATUS").Value != "NEEDS-ACTION" {
		t.Errorf("STATUS mismatch: got %q", todo.Props.Get("STATUS").Value)
	}

	// Verify VTODO DUE matches task due_time
	if todo.Props.Get("DUE") == nil {
		t.Error("DUE property is missing")
	}
}

// TestIntegration_AC7_2_CalDAVVTODOSyncsToTablet verifies AC7.2:
// VTODO created via CalDAV client syncs to tablet on next sync
func TestIntegration_AC7_2_CalDAVVTODOSyncsToTablet(t *testing.T) {
	// Setup: in-memory database with event bus and notifier
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	eventBus := events.NewEventBus()

	// Track notifications
	notificationReceived := false
	eventBus.Subscribe(events.FileModified, func(event events.Event) {
		if event.UserID == userID {
			notificationReceived = true
		}
	})

	// Create notifier adapter
	notifier := &eventBusNotifier{bus: eventBus, userID: userID}
	backend := NewBackend(store, "/caldav", "Supernote Tasks", "preserve", notifier)
	ctx := context.Background()

	// AC7.2 Part 1: Create a VTODO via CalDAV
	cal := ical.NewCalendar()
	cal.Props.SetText("PRODID", "-//Test//Test//EN")
	cal.Props.SetText("VERSION", "2.0")

	todo := ical.NewComponent("VTODO")
	todo.Props.SetText("UID", "caldav-task-1")
	todo.Props.SetText("SUMMARY", "Call Doctor")
	todo.Props.SetText("STATUS", "NEEDS-ACTION")
	todo.Props.SetText("DESCRIPTION", "Schedule annual checkup")
	cal.Children = append(cal.Children, todo)

	_, err := backend.PutCalendarObject(ctx, "/caldav/user/calendars/tasks/caldav-task-1.ics", cal, nil)
	if err != nil {
		t.Fatalf("PutCalendarObject failed: %v", err)
	}

	// AC7.2 Part 2: Verify event bus was notified (would trigger Socket.IO push to tablet)
	time.Sleep(10 * time.Millisecond) // Give goroutines time to run
	if !notificationReceived {
		t.Error("Event bus was not notified of task creation")
	}

	// AC7.2 Part 3: List tasks via store (simulating tablet fetch on next sync)
	tasks, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Verify new task has correct fields
	newTask := tasks[0]
	if newTask.TaskID != "caldav-task-1" {
		t.Errorf("TaskID mismatch: got %q", newTask.TaskID)
	}
	if taskstore.NullStr(newTask.Title) != "Call Doctor" {
		t.Errorf("Title mismatch: got %q", taskstore.NullStr(newTask.Title))
	}
	if taskstore.NullStr(newTask.Status) != "needsAction" {
		t.Errorf("Status mismatch: got %q", taskstore.NullStr(newTask.Status))
	}
	if taskstore.NullStr(newTask.Detail) != "Schedule annual checkup" {
		t.Errorf("Detail mismatch: got %q", taskstore.NullStr(newTask.Detail))
	}
}

// TestIntegration_AC7_3_CompletionStatusRoundTrip verifies AC7.3:
// Task completion status round-trips: tablet ↔ CalDAV
func TestIntegration_AC7_3_CompletionStatusRoundTrip(t *testing.T) {
	// Setup
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	backend := NewBackend(store, "/caldav", "Supernote Tasks", "preserve", nil)
	ctx := context.Background()

	// Step 1: Create task via store (tablet sync) with status "needsAction"
	taskID := "roundtrip-1"
	task := &taskstore.Task{
		TaskID:       taskID,
		TaskListID:   taskstore.SqlStr("default-list"),
		Title:        taskstore.SqlStr("Review Proposal"),
		Status:       taskstore.SqlStr("needsAction"),
		LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Step 2: Read via CalDAV → STATUS should be NEEDS-ACTION
	caldavObj, err := backend.GetCalendarObject(ctx, fmt.Sprintf("/caldav/user/calendars/tasks/%s.ics", taskID), nil)
	if err != nil {
		t.Fatalf("GetCalendarObject failed: %v", err)
	}

	caldavTodo, err := FindVTODO(caldavObj.Data)
	if err != nil {
		t.Fatalf("FindVTODO failed: %v", err)
	}

	if caldavTodo.Props.Get("STATUS").Value != "NEEDS-ACTION" {
		t.Errorf("Initial STATUS mismatch: got %q", caldavTodo.Props.Get("STATUS").Value)
	}

	// Step 3: Update via CalDAV: set STATUS to COMPLETED
	time.Sleep(1 * time.Millisecond)
	cal := ical.NewCalendar()
	cal.Props.SetText("PRODID", "-//Test//Test//EN")
	cal.Props.SetText("VERSION", "2.0")

	updatedTodo := ical.NewComponent("VTODO")
	updatedTodo.Props.SetText("UID", taskID)
	updatedTodo.Props.SetText("SUMMARY", "Review Proposal")
	updatedTodo.Props.SetText("STATUS", "COMPLETED")
	cal.Children = append(cal.Children, updatedTodo)

	_, err = backend.PutCalendarObject(ctx, fmt.Sprintf("/caldav/user/calendars/tasks/%s.ics", taskID), cal, nil)
	if err != nil {
		t.Fatalf("PutCalendarObject (update to completed) failed: %v", err)
	}

	// Step 4: Read via store → status should be "completed"
	updatedTask, err := store.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if taskstore.NullStr(updatedTask.Status) != "completed" {
		t.Errorf("Status after CalDAV update: got %q, want %q", taskstore.NullStr(updatedTask.Status), "completed")
	}

	// Step 5: Update via store: set status back to "needsAction"
	time.Sleep(1 * time.Millisecond)
	updatedTask.Status = taskstore.SqlStr("needsAction")
	if err := store.Update(ctx, updatedTask); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Step 6: Read via CalDAV → STATUS should be NEEDS-ACTION again
	finalObj, err := backend.GetCalendarObject(ctx, fmt.Sprintf("/caldav/user/calendars/tasks/%s.ics", taskID), nil)
	if err != nil {
		t.Fatalf("GetCalendarObject (final) failed: %v", err)
	}

	finalTodo, err := FindVTODO(finalObj.Data)
	if err != nil {
		t.Fatalf("FindVTODO (final) failed: %v", err)
	}

	if finalTodo.Props.Get("STATUS").Value != "NEEDS-ACTION" {
		t.Errorf("Final STATUS mismatch: got %q", finalTodo.Props.Get("STATUS").Value)
	}
}

// TestIntegration_MultipleTasksSync verifies multiple tasks sync correctly
func TestIntegration_MultipleTasksSync(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	backend := NewBackend(store, "/caldav", "Supernote Tasks", "preserve", nil)
	ctx := context.Background()

	// Create 3 tasks via store
	for i := 1; i <= 3; i++ {
		task := &taskstore.Task{
			TaskID:       fmt.Sprintf("task-%d", i),
			TaskListID:   taskstore.SqlStr("default-list"),
			Title:        taskstore.SqlStr(fmt.Sprintf("Task %d", i)),
			Status:       taskstore.SqlStr("needsAction"),
			LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
		}
		if err := store.Create(ctx, task); err != nil {
			t.Fatalf("Create task %d failed: %v", i, err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	// List via CalDAV
	objects, err := backend.ListCalendarObjects(ctx, "/caldav/user/calendars/tasks/", nil)
	if err != nil {
		t.Fatalf("ListCalendarObjects failed: %v", err)
	}

	if len(objects) != 3 {
		t.Errorf("Expected 3 objects, got %d", len(objects))
	}

	// Verify each one
	for i := 1; i <= 3; i++ {
		found := false
		for _, obj := range objects {
			todo, err := FindVTODO(obj.Data)
			if err == nil && todo.Props.Get("SUMMARY").Value == fmt.Sprintf("Task %d", i) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Task %d not found in CalDAV list", i)
		}
	}
}

// TestIntegration_TaskDeletion verifies soft-delete behavior
func TestIntegration_TaskDeletion(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	backend := NewBackend(store, "/caldav", "Supernote Tasks", "preserve", nil)
	ctx := context.Background()

	// Create a task
	task := &taskstore.Task{
		TaskID:       "delete-me",
		TaskListID:   taskstore.SqlStr("default-list"),
		Title:        taskstore.SqlStr("Temporary Task"),
		Status:       taskstore.SqlStr("needsAction"),
		LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	if err := store.Create(ctx, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Verify it appears in CalDAV
	objects, _ := backend.ListCalendarObjects(ctx, "/caldav/user/calendars/tasks/", nil)
	if len(objects) != 1 {
		t.Errorf("Expected 1 task, got %d", len(objects))
	}

	// Delete via CalDAV
	err := backend.DeleteCalendarObject(ctx, "/caldav/user/calendars/tasks/delete-me.ics")
	if err != nil {
		t.Fatalf("DeleteCalendarObject failed: %v", err)
	}

	// Verify it's gone from CalDAV list
	objects, _ = backend.ListCalendarObjects(ctx, "/caldav/user/calendars/tasks/", nil)
	if len(objects) != 0 {
		t.Errorf("Expected 0 tasks after delete, got %d", len(objects))
	}

	// Verify soft-delete in database
	row := db.QueryRowContext(ctx,
		"SELECT is_deleted FROM schedule_tasks WHERE task_id = ? AND user_id = ?",
		"delete-me", userID)
	var isDeleted string
	if err := row.Scan(&isDeleted); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if isDeleted != "Y" {
		t.Errorf("is_deleted should be 'Y', got %q", isDeleted)
	}
}

// TestIntegration_CTAGChanges verifies CTag changes correctly
func TestIntegration_CTAGChanges(t *testing.T) {
	db, userID := setupTestDB(t)
	defer db.Close()

	store := taskstore.New(db, userID)
	ctx := context.Background()

	// Initial CTag
	tasks, _ := store.List(ctx)
	ctag1 := taskstore.ComputeCTag(tasks)

	// Create task 1
	time.Sleep(1 * time.Millisecond)
	task1 := &taskstore.Task{
		TaskID:       "ctag-1",
		TaskListID:   taskstore.SqlStr("default-list"),
		Title:        taskstore.SqlStr("Task 1"),
		Status:       taskstore.SqlStr("needsAction"),
		LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	store.Create(ctx, task1)

	tasks, _ = store.List(ctx)
	ctag2 := taskstore.ComputeCTag(tasks)

	if ctag1 == ctag2 {
		t.Error("CTag should change after creating task 1")
	}

	// Create task 2
	time.Sleep(1 * time.Millisecond)
	task2 := &taskstore.Task{
		TaskID:       "ctag-2",
		TaskListID:   taskstore.SqlStr("default-list"),
		Title:        taskstore.SqlStr("Task 2"),
		Status:       taskstore.SqlStr("needsAction"),
		LastModified: sql.NullInt64{Int64: time.Now().UnixMilli(), Valid: true},
	}
	store.Create(ctx, task2)

	tasks, _ = store.List(ctx)
	ctag3 := taskstore.ComputeCTag(tasks)

	if ctag2 == ctag3 {
		t.Error("CTag should change after creating task 2")
	}

	if ctag1 == ctag3 {
		t.Error("CTag1 should differ from CTag3")
	}
}

// eventBusNotifier publishes sync notifications via event bus
type eventBusNotifier struct {
	bus    *events.EventBus
	userID int64
}

func (n *eventBusNotifier) Notify(ctx context.Context) error {
	n.bus.Publish(events.Event{
		Type:   events.FileModified,
		UserID: n.userID,
	})
	return nil
}

// Helper to setup test database with user and task list
func setupTestDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()

	db, err := syncdb.Open(":memory:")
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}

	// Insert test user
	result, err := db.Exec(
		"INSERT INTO users (email, password_hash, username) VALUES (?, ?, ?)",
		"test@example.com",
		"$2a$10$...",
		"testuser",
	)
	if err != nil {
		t.Fatalf("Failed to insert user: %v", err)
	}

	userID, _ := result.LastInsertId()

	// Create both default-list and default task lists
	for _, listID := range []string{"default-list", "default"} {
		_, err = db.Exec(
			"INSERT INTO schedule_groups (task_list_id, user_id, title) VALUES (?, ?, ?)",
			listID,
			userID,
			"Tasks",
		)
		if err != nil {
			t.Fatalf("Failed to create task list %q: %v", listID, err)
		}
	}

	return db, userID
}
