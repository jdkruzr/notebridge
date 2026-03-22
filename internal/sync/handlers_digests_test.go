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
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var summaryResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&summaryResp)
	summaryID := summaryResp["id"].(float64)

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
	summaries := queryResp["summaries"].([]interface{})

	if len(summaries) < 1 {
		t.Fatalf("expected at least 1 summary")
	}

	foundSummary := false
	for _, s := range summaries {
		summary := s.(map[string]interface{})
		if summary["id"].(float64) == summaryID {
			if summary["name"].(string) != "Meeting Notes" {
				t.Fatalf("expected name 'Meeting Notes', got %s", summary["name"].(string))
			}
			foundSummary = true
			break
		}
	}

	if !foundSummary {
		t.Fatalf("created summary not found in query results")
	}

	// Step 3: Update summary
	updateBody := map[string]interface{}{
		"id":               int64(summaryID),
		"name":             "Updated Meeting Notes",
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	updateBodyBytes, _ := json.Marshal(updateBody)

	req = httptest.NewRequest("PUT", "/api/file/update/summary", bytes.NewReader(updateBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 4: Delete summary
	deleteBody := map[string]interface{}{
		"id": int64(summaryID),
	}
	deleteBodyBytes, _ := json.Marshal(deleteBody)

	req = httptest.NewRequest("DELETE", "/api/file/delete/summary", bytes.NewReader(deleteBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
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
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var groupResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&groupResp)
	groupID := groupResp["id"].(float64)

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
	groups := listResp["summaryDOList"].([]interface{})

	if len(groups) < 1 {
		t.Fatalf("expected at least 1 group")
	}

	// Step 3: Update group
	updateBody := map[string]interface{}{
		"id":               int64(groupID),
		"name":             "Updated Project Alpha",
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	updateBodyBytes, _ := json.Marshal(updateBody)

	req = httptest.NewRequest("PUT", "/api/file/update/summary/group", bytes.NewReader(updateBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 4: Delete group
	deleteBody := map[string]interface{}{
		"id": int64(groupID),
	}
	deleteBodyBytes, _ := json.Marshal(deleteBody)

	req = httptest.NewRequest("DELETE", "/api/file/delete/summary/group", bytes.NewReader(deleteBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// Step 5: Verify deletion
	req = httptest.NewRequest("POST", "/api/file/query/summary/group", bytes.NewReader(listBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	json.NewDecoder(w.Body).Decode(&listResp)
	groups = listResp["summaryDOList"].([]interface{})

	if len(groups) != 0 {
		t.Fatalf("expected 0 groups after deletion, got %d", len(groups))
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

	innerName := uploadResp["innerName"].(string)

	// Step 2: Create summary with handwriteInnerName
	summaryBody := map[string]interface{}{
		"uniqueIdentifier":  "summary-upload-001",
		"name":              "Uploaded Summary",
		"handwriteInnerName": innerName,
		"creationTime":      time.Now().UnixMilli(),
		"lastModifiedTime":  time.Now().UnixMilli(),
	}
	summaryBodyBytes, _ := json.Marshal(summaryBody)

	req = httptest.NewRequest("POST", "/api/file/add/summary", bytes.NewReader(summaryBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var summaryResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&summaryResp)
	summaryID := summaryResp["id"].(float64)

	// Step 3: Download - get signed download URL
	downloadBody := map[string]interface{}{
		"id": int64(summaryID),
	}
	downloadBodyBytes, _ := json.Marshal(downloadBody)

	req = httptest.NewRequest("POST", "/api/file/download/summary", bytes.NewReader(downloadBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var downloadResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&downloadResp)

	if downloadResp["url"] == nil {
		t.Fatalf("expected url in download response")
	}
}

// TestAC63DownloadNoFile tests AC6.3: download fails if handwriteInnerName not set
func TestAC63DownloadNoFile(t *testing.T) {
	server, store := setupTestServer(t)

	// Generate JWT token
	token, err := generateTestToken(server, store)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Step 1: Create summary without handwriteInnerName
	summaryBody := map[string]interface{}{
		"uniqueIdentifier": "summary-no-file",
		"name":             "Summary Without File",
		"creationTime":     time.Now().UnixMilli(),
		"lastModifiedTime": time.Now().UnixMilli(),
	}
	summaryBodyBytes, _ := json.Marshal(summaryBody)

	req := httptest.NewRequest("POST", "/api/file/add/summary", bytes.NewReader(summaryBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	var summaryResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&summaryResp)
	summaryID := summaryResp["id"].(float64)

	// Step 2: Try to download - should fail
	downloadBody := map[string]interface{}{
		"id": int64(summaryID),
	}
	downloadBodyBytes, _ := json.Marshal(downloadBody)

	req = httptest.NewRequest("POST", "/api/file/download/summary", bytes.NewReader(downloadBodyBytes))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	server.Config.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}

	var errResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&errResp)
	if errResp["cd"].(string) != "E0340" {
		t.Fatalf("expected error code E0340, got %s", errResp["cd"].(string))
	}
}
