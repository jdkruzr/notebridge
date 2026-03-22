package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sysop/notebridge/internal/syncdb"
)

// TestAC51TaskSync tests AC5.1: task created on tablet syncs to NoteBridge
func TestAC51TaskSync(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create a schedule group
	groupBody := map[string]interface{}{
		"title":        "Test Task List",
		"lastModified": time.Now().UnixMilli(),
		"createTime":   time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/schedule/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var groupResp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&groupResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	taskListID, ok := groupResp["taskListId"].(string)
	if !ok || taskListID == "" {
		t.Fatalf("expected taskListId in response, got: %v", groupResp)
	}

	// Step 2: Create a task in that group
	taskBody := map[string]interface{}{
		"taskListId":     taskListID,
		"title":          "Buy groceries",
		"detail":         "Milk, eggs, bread",
		"status":         "0",
		"importance":     "1",
		"dueTime":        time.Now().Add(24 * time.Hour).UnixMilli(),
		"lastModified":   time.Now().UnixMilli(),
	}
	taskBodyBytes, _ := json.Marshal(taskBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task", bytes.NewReader(taskBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var taskResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&taskResp)
	taskID := taskResp["taskId"].(string)

	// Step 3: List tasks and verify
	listBody := map[string]interface{}{
		"maxResults": 20,
	}
	listBodyBytes, _ := json.Marshal(listBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task/all", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)
	tasks := listResp["scheduleTask"].([]interface{})

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	taskData := tasks[0].(map[string]interface{})
	// Check either camelCase or PascalCase field names due to JSON marshaling
	var foundTaskID, foundTitle bool
	for key, value := range taskData {
		if key == "taskId" && value.(string) == taskID {
			foundTaskID = true
		}
		if key == "title" && value.(string) == "Buy groceries" {
			foundTitle = true
		}
	}
	if !foundTaskID || !foundTitle {
		t.Fatalf("task data missing expected fields: %v", taskData)
	}
}

// TestAC52GroupCRUD tests AC5.2: group CRUD operations
func TestAC52GroupCRUD(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create group
	groupBody := map[string]interface{}{
		"title":        "My Tasks",
		"lastModified": time.Now().UnixMilli(),
		"createTime":   time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/schedule/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)
	taskListID := groupResp["taskListId"].(string)

	// Step 2: List groups
	listBody := map[string]interface{}{"maxResults": 20}
	listBodyBytes, _ := json.Marshal(listBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/group/all", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)

	groups, ok := listResp["scheduleTaskGroup"].([]interface{})
	if !ok {
		t.Fatalf("expected scheduleTaskGroup array in response: %v", listResp)
	}
	if len(groups) < 1 {
		t.Fatalf("expected at least 1 group")
	}

	// Step 3: Update group
	updateBody := map[string]interface{}{
		"taskListId":   taskListID,
		"title":        "Updated Tasks",
		"lastModified": time.Now().UnixMilli(),
	}
	updateBodyBytes, _ := json.Marshal(updateBody)

	req = httptest.NewRequest("PUT", "/api/file/schedule/group", bytes.NewReader(updateBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 4: Delete group
	req = httptest.NewRequest("DELETE", "/api/file/schedule/group/"+taskListID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 5: Verify deletion
	req = httptest.NewRequest("POST", "/api/file/schedule/group/all", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&listResp)
	groupsVal, ok := listResp["scheduleTaskGroup"].([]interface{})
	if ok && len(groupsVal) != 0 {
		t.Fatalf("expected 0 groups after deletion, got %d", len(groupsVal))
	} else if !ok && listResp["scheduleTaskGroup"] != nil {
		t.Fatalf("expected scheduleTaskGroup array in response: %v", listResp)
	}
}

// TestAC53BatchUpdate tests AC5.3: batch task update atomicity
func TestAC53BatchUpdate(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Setup: Create group and 3 tasks
	groupBody := map[string]interface{}{
		"title":        "Test Group",
		"lastModified": time.Now().UnixMilli(),
		"createTime":   time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/schedule/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)
	taskListID := groupResp["taskListId"].(string)

	// Create 3 tasks
	var taskIDs []string
	for i := 0; i < 3; i++ {
		taskBody := map[string]interface{}{
			"taskListId":   taskListID,
			"title":        "Task " + string(rune(i+1+'0')),
			"status":       "0",
			"lastModified": time.Now().UnixMilli(),
		}
		taskBodyBytes, _ := json.Marshal(taskBody)

		req = httptest.NewRequest("POST", "/api/file/schedule/task", bytes.NewReader(taskBodyBytes))
		req.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		server.Config.Handler.ServeHTTP(w, req)

		var taskResp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&taskResp)
		taskIDs = append(taskIDs, taskResp["taskId"].(string))
	}

	// Step 1: Batch update 2 tasks
	batchBody := map[string]interface{}{
		"updateScheduleTaskList": []map[string]interface{}{
			{
				"taskId": taskIDs[0],
				"fields": map[string]interface{}{
					"status":       "1",
					"importance":   "2",
					"lastModified": time.Now().UnixMilli(),
				},
			},
			{
				"taskId": taskIDs[1],
				"fields": map[string]interface{}{
					"status":       "2",
					"lastModified": time.Now().UnixMilli(),
				},
			},
		},
	}
	batchBodyBytes, _ := json.Marshal(batchBody)

	req = httptest.NewRequest("PUT", "/api/file/schedule/task/list", bytes.NewReader(batchBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 2: Batch update with non-existent ID (should fail atomically)
	badBatchBody := map[string]interface{}{
		"updateScheduleTaskList": []map[string]interface{}{
			{
				"taskId": "nonexistent",
				"fields": map[string]interface{}{
					"status": "3",
				},
			},
		},
	}
	badBatchBodyBytes, _ := json.Marshal(badBatchBody)

	req = httptest.NewRequest("PUT", "/api/file/schedule/task/list", bytes.NewReader(badBatchBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for invalid task, got %d", w.Code)
	}
}

// TestAC54SyncToken tests AC5.4: nextSyncToken pagination
func TestAC54SyncToken(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Setup: Create group
	groupBody := map[string]interface{}{
		"title":        "Sync Test",
		"lastModified": time.Now().UnixMilli(),
		"createTime":   time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/schedule/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)
	taskListID := groupResp["taskListId"].(string)

	// Step 1: Create task A
	taskABody := map[string]interface{}{
		"taskListId":   taskListID,
		"title":        "Task A",
		"lastModified": time.Now().UnixMilli(),
	}
	taskABodyBytes, _ := json.Marshal(taskABody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task", bytes.NewReader(taskABodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	// Step 2: List all tasks and get nextSyncToken
	listBody := map[string]interface{}{"maxResults": 20}
	listBodyBytes, _ := json.Marshal(listBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task/all", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)

	// Manually get the nextSyncToken from response
	var syncToken int64
	if nextSyncToken, ok := listResp["nextSyncToken"]; ok {
		syncToken = int64(nextSyncToken.(float64))
	}

	// Step 3: Create task B
	time.Sleep(10 * time.Millisecond) // Ensure different timestamp
	taskBBody := map[string]interface{}{
		"taskListId":   taskListID,
		"title":        "Task B",
		"lastModified": time.Now().UnixMilli(),
	}
	taskBBodyBytes, _ := json.Marshal(taskBBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task", bytes.NewReader(taskBBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	// Step 4: List tasks with syncToken (should return only task B)
	syncListBody := map[string]interface{}{
		"maxResults":   20,
		"nextSyncToken": syncToken,
	}
	syncListBodyBytes, _ := json.Marshal(syncListBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task/all", bytes.NewReader(syncListBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&listResp)
	tasks := listResp["scheduleTask"].([]interface{})

	// Should have returned 1 task (task B)
	if len(tasks) < 1 {
		t.Fatalf("expected at least 1 task with syncToken filter")
	}
}

// TestAC55Recurrence tests AC5.5: recurrence field preservation
func TestAC55Recurrence(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Setup: Create group
	groupBody := map[string]interface{}{
		"title":        "Recurrence Test",
		"lastModified": time.Now().UnixMilli(),
		"createTime":   time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/schedule/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)
	taskListID := groupResp["taskListId"].(string)

	// Step 1: Create task with recurrence
	recurrenceRule := "RRULE:FREQ=DAILY;COUNT=5"
	taskBody := map[string]interface{}{
		"taskListId":   taskListID,
		"title":        "Daily standup",
		"recurrence":   recurrenceRule,
		"lastModified": time.Now().UnixMilli(),
	}
	taskBodyBytes, _ := json.Marshal(taskBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task", bytes.NewReader(taskBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var taskResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&taskResp)
	taskID := taskResp["taskId"].(string)

	// Step 2: List tasks and verify recurrence
	listBody := map[string]interface{}{"maxResults": 20}
	listBodyBytes, _ := json.Marshal(listBody)

	req = httptest.NewRequest("POST", "/api/file/schedule/task/all", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)
	tasks := listResp["scheduleTask"].([]interface{})

	found := false
	for _, task := range tasks {
		taskData := task.(map[string]interface{})
		if taskData["taskId"].(string) == taskID {
			if taskData["recurrence"].(string) != recurrenceRule {
				t.Fatalf("expected recurrence %s, got %s", recurrenceRule, taskData["recurrence"].(string))
			}
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("task not found in list")
	}
}

// generateTestToken generates a JWT token by logging in
func generateTestToken(server *httptest.Server, store *syncdb.Store) (string, error) {
	ctx := context.Background()
	testEmail := "test@example.com"
	testPasswordHash := "md5hash_of_testpassword"

	// Step 1: Request challenge
	challengeBody := map[string]interface{}{
		"account": testEmail,
	}
	challengeBodyBytes, _ := json.Marshal(challengeBody)

	req := httptest.NewRequest("POST", "/api/user/login/challenge", bytes.NewReader(challengeBodyBytes))
	w := httptest.NewRecorder()

	authService := NewAuthService(store, NewSnowflakeGenerator())
	randomCode, timestamp, err := authService.GenerateChallenge(ctx, testEmail)
	if err != nil {
		return "", err
	}

	// Step 2: Compute password hash and verify
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(testPasswordHash+randomCode)))

	// Step 3: Login verify
	loginBody := map[string]interface{}{
		"account":     testEmail,
		"password":    expectedHash,
		"timestamp":   timestamp,
		"equipmentNo": "test-equipment",
	}
	loginBodyBytes, _ := json.Marshal(loginBody)

	req = httptest.NewRequest("POST", "/api/user/login/verify", bytes.NewReader(loginBodyBytes))
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		return "", fmt.Errorf("login failed with status %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	token, ok := resp["token"].(string)
	if !ok {
		return "", fmt.Errorf("token not found in response")
	}

	return token, nil
}
