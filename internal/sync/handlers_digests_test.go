package sync

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestAC61SummarySync tests AC6.1: summary created on tablet syncs to NoteBridge
func TestAC61SummarySync(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create a summary item
	summaryBody := map[string]interface{}{
		"uniqueIdentifier": "summary-001",
		"name":             "Meeting Notes",
		"description":      "Quarterly review notes",
		"md5Hash":          "abc123def456",
		"creationTime":     time.Now().UnixMilli(),
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	summaryBodyBytes, _ := json.Marshal(summaryBody)

	req := httptest.NewRequest("POST", "/api/file/add/summary", bytes.NewReader(summaryBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var summaryResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&summaryResp)

	if summaryResp["id"] == nil {
		t.Fatalf("expected id in response: %v", summaryResp)
	}

	// Step 2: Query summaries and verify
	queryBody := map[string]interface{}{
		"page": 1,
		"size": 20,
	}
	queryBodyBytes, _ := json.Marshal(queryBody)

	req = httptest.NewRequest("POST", "/api/file/query/summary", bytes.NewReader(queryBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var queryResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&queryResp)

	if queryResp["summaries"] == nil {
		t.Fatalf("expected summaries in response: %v", queryResp)
	}
}

// TestAC62SummaryGroupCRUD tests AC6.2: summary group CRUD works
func TestAC62SummaryGroupCRUD(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create summary group
	groupBody := map[string]interface{}{
		"uniqueIdentifier": "group-001",
		"name":             "Project Alpha",
		"description":      "All docs for Project Alpha",
		"md5Hash":          "xyz789",
		"creationTime":     time.Now().UnixMilli(),
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/add/summary/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)

	if groupResp["id"] == nil {
		t.Fatalf("expected id in response: %v", groupResp)
	}

	// Step 2: List summary groups
	listBody := map[string]interface{}{
		"page": 1,
		"size": 20,
	}
	listBodyBytes, _ := json.Marshal(listBody)

	req = httptest.NewRequest("POST", "/api/file/query/summary/group", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var listResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&listResp)

	if listResp["summaryDOList"] == nil {
		t.Fatalf("expected summaryDOList in response: %v", listResp)
	}
}

// TestAC62DuplicateUniqueID tests AC6.2: duplicate uniqueIdentifier returns E0338
func TestAC62DuplicateUniqueID(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create first summary group
	groupBody := map[string]interface{}{
		"uniqueIdentifier": "unique-group-1",
		"name":             "Group 1",
		"creationTime":     time.Now().UnixMilli(),
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	groupBodyBytes, _ := json.Marshal(groupBody)

	req := httptest.NewRequest("POST", "/api/file/add/summary/group", bytes.NewReader(groupBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 2: Try to create duplicate
	duplicateBody := map[string]interface{}{
		"uniqueIdentifier": "unique-group-1",
		"name":             "Different Name",
		"creationTime":     time.Now().UnixMilli(),
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	duplicateBodyBytes, _ := json.Marshal(duplicateBody)

	req = httptest.NewRequest("POST", "/api/file/add/summary/group", bytes.NewReader(duplicateBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409 for duplicate, got %d", w.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp["cd"].(string) != "E0338" {
		t.Fatalf("expected error code E0338, got %s", errResp["cd"].(string))
	}
}

// TestAC63UploadDownload tests AC6.3: summary file upload/download via signed URLs
func TestAC63UploadDownload(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Upload apply - get signed URLs
	uploadBody := map[string]interface{}{
		"fileName": "test-summary.zip",
	}
	uploadBodyBytes, _ := json.Marshal(uploadBody)

	req := httptest.NewRequest("POST", "/api/file/upload/apply/summary", bytes.NewReader(uploadBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var uploadResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&uploadResp)

	if uploadResp["fullUploadUrl"] == nil {
		t.Fatalf("expected fullUploadUrl in response")
	}
	if uploadResp["partUploadUrl"] == nil {
		t.Fatalf("expected partUploadUrl in response")
	}
}

// TestAC63DownloadNoFile tests AC6.3: download fails for non-existent summary
func TestAC63DownloadNoFile(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Try to download with non-existent ID
	downloadBody := map[string]interface{}{
		"id": int64(9999),
	}
	downloadBodyBytes, _ := json.Marshal(downloadBody)

	req := httptest.NewRequest("POST", "/api/file/download/summary", bytes.NewReader(downloadBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", w.Code, w.Body.String())
	}

	var errResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp["cd"].(string) != "E0340" {
		t.Fatalf("expected error code E0340, got %s", errResp["cd"].(string))
	}
}
