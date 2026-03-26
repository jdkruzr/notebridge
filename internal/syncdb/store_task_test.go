package syncdb

import (
	"context"
	"testing"
	"time"
)

// TestUpsertScheduleGroup creates group and updates it on second call.
func TestUpsertScheduleGroup(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create group
	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Verify it was created
	retrievedGroups, count, err := store.ListScheduleGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 group, got %d", count)
	}

	if retrievedGroups[0].Title != "My Tasks" {
		t.Errorf("expected title 'My Tasks', got '%s'", retrievedGroups[0].Title)
	}

	// Update it
	group.Title = "Updated Tasks"
	group.LastModified = 2000

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to update group: %v", err)
	}

	// Verify update
	retrievedGroups, count, err = store.ListScheduleGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 group, got %d", count)
	}

	if retrievedGroups[0].Title != "Updated Tasks" {
		t.Errorf("expected updated title 'Updated Tasks', got '%s'", retrievedGroups[0].Title)
	}
}

// TestUpsertScheduleGroupAutoGenerateID auto-generates ID from title.
func TestUpsertScheduleGroupAutoGenerateID(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create group without ID
	group := &ScheduleGroup{
		TaskListID:   "", // Empty, should be auto-generated
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	if group.TaskListID == "" {
		t.Errorf("expected auto-generated ID, got empty")
	}

	// Verify it was created
	_, count, err := store.ListScheduleGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 group, got %d", count)
	}
}

// TestUpdateScheduleGroup with non-existent ID returns error.
func TestUpdateScheduleGroupNotFound(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Try to update non-existent group
	err = store.UpdateScheduleGroup(ctx, "nonexistent", user.ID, map[string]interface{}{"title": "New Title"})

	if err != ErrTaskGroupNotFound {
		t.Errorf("expected ErrTaskGroupNotFound, got %v", err)
	}
}

// TestDeleteScheduleGroupCascade deletes group and all its tasks.
func TestDeleteScheduleGroupCascade(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create group
	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create tasks in group
	task1 := &ScheduleTask{
		TaskID:       "t1",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task 1",
		LastModified: 1000,
	}

	task2 := &ScheduleTask{
		TaskID:       "t2",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task 2",
		LastModified: 1100,
	}

	err = store.UpsertScheduleTask(ctx, task1)
	if err != nil {
		t.Fatalf("failed to upsert task 1: %v", err)
	}

	err = store.UpsertScheduleTask(ctx, task2)
	if err != nil {
		t.Fatalf("failed to upsert task 2: %v", err)
	}

	// List tasks
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}

	// Delete group (should cascade)
	err = store.DeleteScheduleGroup(ctx, "task1", user.ID)
	if err != nil {
		t.Fatalf("failed to delete group: %v", err)
	}

	// Verify group is gone
	_, count, err := store.ListScheduleGroups(ctx, user.ID, 1, 10)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}

	if count != 0 {
		t.Errorf("expected 0 groups, got %d", count)
	}

	// Verify tasks are gone
	tasks, _, _, err = store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

// TestListScheduleGroupsPagination creates 5 groups, pages 1 size 2 returns 2, page 3 returns 1.
func TestListScheduleGroupsPagination(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Create 5 groups
	for i := 1; i <= 5; i++ {
		group := &ScheduleGroup{
			TaskListID:   "task" + string(rune('0'+i)),
			UserID:       user.ID,
			Title:        "Task List " + string(rune('0'+i)),
			LastModified: 1000 + int64(i),
			CreateTime:   900,
		}

		err := store.UpsertScheduleGroup(ctx, group)
		if err != nil {
			t.Fatalf("failed to upsert group: %v", err)
		}
	}

	// Page 1, size 2
	groups, count, err := store.ListScheduleGroups(ctx, user.ID, 1, 2)
	if err != nil {
		t.Fatalf("failed to list groups (page 1): %v", err)
	}

	if count != 5 {
		t.Errorf("expected total count 5, got %d", count)
	}

	if len(groups) != 2 {
		t.Errorf("expected 2 groups on page 1, got %d", len(groups))
	}

	// Page 3, size 2
	groups, count, err = store.ListScheduleGroups(ctx, user.ID, 3, 2)
	if err != nil {
		t.Fatalf("failed to list groups (page 3): %v", err)
	}

	if len(groups) != 1 {
		t.Errorf("expected 1 group on page 3, got %d", len(groups))
	}
}

// TestUpsertScheduleTask creates task with all fields.
func TestUpsertScheduleTask(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create task
	task := &ScheduleTask{
		TaskID:           "t1",
		UserID:           user.ID,
		TaskListID:       "task1",
		Title:            "Task 1",
		Detail:           "Details",
		Status:           "pending",
		Importance:       "high",
		Recurrence:       "RRULE:FREQ=DAILY",
		Links:            "link1,link2",
		IsReminderOn:     "Y",
		DueTime:          1500,
		CompletedTime:    0,
		LastModified:     1400,
		Sort:             1,
		SortCompleted:    0,
		PlanerSort:       2,
		SortTime:         3,
		PlanerSortTime:   4,
		AllSort:          5,
		AllSortCompleted: 0,
		AllSortTime:      6,
		RecurrenceID:     "recur1",
	}

	err = store.UpsertScheduleTask(ctx, task)
	if err != nil {
		t.Fatalf("failed to upsert task: %v", err)
	}

	// Retrieve and verify
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	retrieved := tasks[0]
	if retrieved.Title != "Task 1" {
		t.Errorf("expected title 'Task 1', got '%s'", retrieved.Title)
	}

	if retrieved.Detail != "Details" {
		t.Errorf("expected detail 'Details', got '%s'", retrieved.Detail)
	}

	if retrieved.Status != "pending" {
		t.Errorf("expected status 'pending', got '%s'", retrieved.Status)
	}

	if retrieved.RecurrenceID != "recur1" {
		t.Errorf("expected recurrence_id 'recur1', got '%s'", retrieved.RecurrenceID)
	}
}

// TestUpsertScheduleTaskAutoGenerateID generates random ID.
func TestUpsertScheduleTaskAutoGenerateID(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create task without ID
	task := &ScheduleTask{
		TaskID:       "", // Empty, should be auto-generated
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task 1",
		LastModified: 1400,
	}

	err = store.UpsertScheduleTask(ctx, task)
	if err != nil {
		t.Fatalf("failed to upsert task: %v", err)
	}

	if task.TaskID == "" {
		t.Errorf("expected auto-generated ID, got empty")
	}
}

// TestUpsertScheduleTaskInvalidGroup returns error.
func TestUpsertScheduleTaskInvalidGroup(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	// Try to create task in non-existent group
	task := &ScheduleTask{
		TaskID:       "t1",
		UserID:       user.ID,
		TaskListID:   "nonexistent",
		Title:        "Task 1",
		LastModified: 1400,
	}

	err = store.UpsertScheduleTask(ctx, task)

	if err != ErrTaskGroupNotFound {
		t.Errorf("expected ErrTaskGroupNotFound, got %v", err)
	}
}

// TestBatchUpdateTasksAtomic updates multiple tasks atomically.
func TestBatchUpdateTasksAtomic(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create 3 tasks
	for i := 1; i <= 3; i++ {
		task := &ScheduleTask{
			TaskID:       "t" + string(rune('0'+i)),
			UserID:       user.ID,
			TaskListID:   "task1",
			Title:        "Task " + string(rune('0'+i)),
			Status:       "pending",
			LastModified: 1400,
		}

		err := store.UpsertScheduleTask(ctx, task)
		if err != nil {
			t.Fatalf("failed to upsert task: %v", err)
		}
	}

	// Batch update 2 tasks
	updates := []TaskUpdate{
		{
			TaskID: "t1",
			Fields: map[string]interface{}{"status": "completed"},
		},
		{
			TaskID: "t2",
			Fields: map[string]interface{}{"status": "in_progress"},
		},
	}

	err = store.BatchUpdateTasks(ctx, user.ID, updates)
	if err != nil {
		t.Fatalf("failed to batch update tasks: %v", err)
	}

	// Verify updates
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	// Find t1 and verify status
	var t1Status string
	var t2Status string
	var t3Status string
	for _, task := range tasks {
		switch task.TaskID {
		case "t1":
			t1Status = task.Status
		case "t2":
			t2Status = task.Status
		case "t3":
			t3Status = task.Status
		}
	}

	if t1Status != "completed" {
		t.Errorf("expected t1 status 'completed', got '%s'", t1Status)
	}

	if t2Status != "in_progress" {
		t.Errorf("expected t2 status 'in_progress', got '%s'", t2Status)
	}

	if t3Status != "pending" {
		t.Errorf("expected t3 status unchanged 'pending', got '%s'", t3Status)
	}
}

// TestBatchUpdateTasksFailOnMissing fails atomically if one taskId doesn't exist.
func TestBatchUpdateTasksFailOnMissing(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create 1 task
	task := &ScheduleTask{
		TaskID:       "t1",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task 1",
		Status:       "pending",
		LastModified: 1400,
	}

	err = store.UpsertScheduleTask(ctx, task)
	if err != nil {
		t.Fatalf("failed to upsert task: %v", err)
	}

	// Batch update with one non-existent taskId
	updates := []TaskUpdate{
		{
			TaskID: "t1",
			Fields: map[string]interface{}{"status": "completed"},
		},
		{
			TaskID: "nonexistent",
			Fields: map[string]interface{}{"status": "in_progress"},
		},
	}

	err = store.BatchUpdateTasks(ctx, user.ID, updates)

	if err != ErrTaskNotFound {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}

	// Verify t1 was NOT updated (transaction rolled back)
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if tasks[0].Status != "pending" {
		t.Errorf("expected t1 status unchanged 'pending', got '%s'", tasks[0].Status)
	}
}

// TestDeleteScheduleTask removes task.
func TestDeleteScheduleTask(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create task
	task := &ScheduleTask{
		TaskID:       "t1",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task 1",
		LastModified: 1400,
	}

	err = store.UpsertScheduleTask(ctx, task)
	if err != nil {
		t.Fatalf("failed to upsert task: %v", err)
	}

	// Delete task
	err = store.DeleteScheduleTask(ctx, "t1", user.ID)
	if err != nil {
		t.Fatalf("failed to delete task: %v", err)
	}

	// Verify it's gone
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}

// TestListScheduleTasksWithSyncToken filters by updated_at >= syncToken.
func TestListScheduleTasksWithSyncToken(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create task A
	taskA := &ScheduleTask{
		TaskID:       "tA",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task A",
		LastModified: 1000,
	}

	err = store.UpsertScheduleTask(ctx, taskA)
	if err != nil {
		t.Fatalf("failed to upsert task A: %v", err)
	}

	// List all tasks and get nextSyncToken
	tasks, _, nextToken, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	if nextToken == nil {
		t.Fatalf("expected nextSyncToken on first page, got nil")
	}

	// Wait a bit and create task B
	time.Sleep(10 * time.Millisecond)

	taskB := &ScheduleTask{
		TaskID:       "tB",
		UserID:       user.ID,
		TaskListID:   "task1",
		Title:        "Task B",
		LastModified: 1100,
	}

	err = store.UpsertScheduleTask(ctx, taskB)
	if err != nil {
		t.Fatalf("failed to upsert task B: %v", err)
	}

	// List with previous syncToken - should get only task B
	tasks, _, nextToken2, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nextToken)
	if err != nil {
		t.Fatalf("failed to list tasks with sync token: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after sync token, got %d", len(tasks))
	}

	if tasks[0].TaskID != "tB" {
		t.Errorf("expected task B, got task %s", tasks[0].TaskID)
	}

	// List again with new syncToken - should get nothing
	time.Sleep(10 * time.Millisecond)

	tasks, _, _, err = store.ListScheduleTasks(ctx, user.ID, 1, 10, nextToken2)
	if err != nil {
		t.Fatalf("failed to list tasks with new sync token: %v", err)
	}

	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after latest sync token, got %d", len(tasks))
	}
}

// TestListScheduleTasksWithoutSyncToken returns all tasks paginated.
func TestListScheduleTasksWithoutSyncToken(t *testing.T) {
	store := setupTestStore(t)
	ctx := context.Background()

	// Create a user and group
	err := store.EnsureUser(ctx, "test@example.com", "hash", 1000000000000001)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}

	group := &ScheduleGroup{
		TaskListID:   "task1",
		UserID:       user.ID,
		Title:        "My Tasks",
		LastModified: 1000,
		CreateTime:   900,
	}

	err = store.UpsertScheduleGroup(ctx, group)
	if err != nil {
		t.Fatalf("failed to upsert group: %v", err)
	}

	// Create 5 tasks
	for i := 1; i <= 5; i++ {
		task := &ScheduleTask{
			TaskID:       "t" + string(rune('0'+i)),
			UserID:       user.ID,
			TaskListID:   "task1",
			Title:        "Task " + string(rune('0'+i)),
			LastModified: 1000 + int64(i),
		}

		err := store.UpsertScheduleTask(ctx, task)
		if err != nil {
			t.Fatalf("failed to upsert task: %v", err)
		}
	}

	// List all
	tasks, _, _, err := store.ListScheduleTasks(ctx, user.ID, 1, 10, nil)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 5 {
		t.Errorf("expected 5 tasks, got %d", len(tasks))
	}
}
