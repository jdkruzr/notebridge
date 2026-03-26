package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sysop/notebridge/internal/auth"
	"github.com/sysop/notebridge/internal/logging"
	"github.com/sysop/notebridge/internal/notestore"
	"github.com/sysop/notebridge/internal/processor"
	"github.com/sysop/notebridge/internal/search"
	"github.com/sysop/notebridge/internal/taskstore"
)

// handleHealth returns 200 OK for health checks.
// This is a copy of the production handler in cmd/notebridge/main.go.
// The test mux needs its own instance to handle unauthenticated /health requests.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// testServerDependencies holds the mock objects for testing.
type testServerDependencies struct {
	store       *mockTaskStore
	notifier    *mockSyncNotifier
	noteStore   *mockNoteStore
	searchIndex *mockSearchIndex
	processor   *mockProcessor
	scanner     *mockFileScanner
}

// setupWebTestServer creates an in-memory test server with all dependencies.
// Returns the server and dependencies for seeding test data.
func setupWebTestServer(t *testing.T, withAuth bool) (*httptest.Server, *testServerDependencies) {
	t.Helper()

	deps := &testServerDependencies{
		store:       newMockTaskStore(),
		notifier:    &mockSyncNotifier{},
		noteStore:   newMockNoteStore(),
		searchIndex: newMockSearchIndex(),
		processor:   newMockProcessor(),
		scanner:     &mockFileScanner{},
	}

	broadcaster := logging.NewLogBroadcaster()
	logger := logging.Setup(logging.Config{
		Level: "info",
	})

	handler := NewHandler(deps.store, deps.notifier, deps.noteStore, deps.searchIndex, deps.processor, deps.scanner, logger, broadcaster)

	// Health endpoint without auth
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)

	if withAuth {
		authMiddleware := auth.New("admin", "$2a$10$8J.p/PZYq4jdKyP1aFRp8.8E.gQxQQOlLVzf9VLg/8qr/WM.fRVLi") // hash of "password"
		mux.Handle("/", authMiddleware.Wrap(handler))
	} else {
		mux.Handle("/", handler)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(func() { server.Close() })

	return server, deps
}

// mockTaskStore implements TaskStoreInterface.
type mockTaskStore struct {
	tasks map[string]*taskstore.Task
}

func newMockTaskStore() *mockTaskStore {
	return &mockTaskStore{
		tasks: make(map[string]*taskstore.Task),
	}
}

func (m *mockTaskStore) List(ctx context.Context) ([]taskstore.Task, error) {
	var tasks []taskstore.Task
	for _, t := range m.tasks {
		tasks = append(tasks, *t)
	}
	return tasks, nil
}

func (m *mockTaskStore) Get(ctx context.Context, taskID string) (*taskstore.Task, error) {
	if t, ok := m.tasks[taskID]; ok {
		return t, nil
	}
	return nil, taskstore.ErrNotFound
}

func (m *mockTaskStore) Create(ctx context.Context, task *taskstore.Task) error {
	m.tasks[task.TaskID] = task
	return nil
}

func (m *mockTaskStore) Update(ctx context.Context, task *taskstore.Task) error {
	m.tasks[task.TaskID] = task
	return nil
}

func (m *mockTaskStore) Delete(ctx context.Context, taskID string) error {
	delete(m.tasks, taskID)
	return nil
}

// mockSyncNotifier implements SyncNotifier.
type mockSyncNotifier struct {
	notifyCount int
}

func (m *mockSyncNotifier) Notify(ctx context.Context) error {
	m.notifyCount++
	return nil
}

// mockNoteStore implements notestore.NoteStore.
type mockNoteStore struct {
	files map[string][]notestore.NoteFile
}

func newMockNoteStore() *mockNoteStore {
	return &mockNoteStore{
		files: make(map[string][]notestore.NoteFile),
	}
}

func (m *mockNoteStore) Scan(ctx context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockNoteStore) List(ctx context.Context, relPath string) ([]notestore.NoteFile, error) {
	if files, ok := m.files[relPath]; ok {
		return files, nil
	}
	return []notestore.NoteFile{}, nil
}

func (m *mockNoteStore) Get(ctx context.Context, path string) (*notestore.NoteFile, error) {
	return nil, nil
}

func (m *mockNoteStore) UpsertFile(ctx context.Context, path string) error {
	return nil
}

func (m *mockNoteStore) SetHash(ctx context.Context, path, hash string) error {
	return nil
}

func (m *mockNoteStore) GetHash(ctx context.Context, path string) (string, error) {
	return "", nil
}

func (m *mockNoteStore) LookupByHash(ctx context.Context, hash string) (path string, found bool, err error) {
	return "", false, nil
}

func (m *mockNoteStore) TransferJob(ctx context.Context, oldPath, newPath string) error {
	return nil
}

// mockSearchIndex implements search.SearchIndex.
type mockSearchIndex struct {
	results map[string][]search.SearchResult
}

func newMockSearchIndex() *mockSearchIndex {
	return &mockSearchIndex{
		results: make(map[string][]search.SearchResult),
	}
}

func (m *mockSearchIndex) Index(ctx context.Context, doc search.NoteDocument) error {
	return nil
}

func (m *mockSearchIndex) Search(ctx context.Context, query search.SearchQuery) ([]search.SearchResult, error) {
	if results, ok := m.results[query.Text]; ok {
		return results, nil
	}
	return []search.SearchResult{}, nil
}

func (m *mockSearchIndex) Delete(ctx context.Context, path string) error {
	return nil
}

func (m *mockSearchIndex) IndexPage(ctx context.Context, path string, pageIdx int, source, bodyText, titleText, keywords string) error {
	return nil
}

func (m *mockSearchIndex) GetContent(ctx context.Context, path string) ([]search.NoteDocument, error) {
	return []search.NoteDocument{}, nil
}

// mockProcessor implements processor.Processor.
type mockProcessor struct {
	running bool
	jobs    map[string]*processor.Job
}

func newMockProcessor() *mockProcessor {
	return &mockProcessor{
		running: false,
		jobs:    make(map[string]*processor.Job),
	}
}

func (m *mockProcessor) Start(ctx context.Context) error {
	m.running = true
	return nil
}

func (m *mockProcessor) Stop() error {
	m.running = false
	return nil
}

func (m *mockProcessor) Status() processor.ProcessorStatus {
	return processor.ProcessorStatus{
		Running:  m.running,
		Pending:  2,
		InFlight: 1,
	}
}

func (m *mockProcessor) Enqueue(ctx context.Context, path string, opts ...processor.EnqueueOption) error {
	return nil
}

func (m *mockProcessor) Skip(ctx context.Context, path string, reason string) error {
	return nil
}

func (m *mockProcessor) Unskip(ctx context.Context, path string) error {
	return nil
}

func (m *mockProcessor) GetJob(ctx context.Context, path string) (*processor.Job, error) {
	if job, ok := m.jobs[path]; ok {
		return job, nil
	}
	return nil, nil
}

// mockFileScanner implements FileScanner.
type mockFileScanner struct {
	scanCount int
}

func (m *mockFileScanner) ScanNow(ctx context.Context) {
	m.scanCount++
}

// AC8.1: File browser shows files
func TestFileBrowser(t *testing.T) {
	server, deps := setupWebTestServer(t, false)

	// Seed mock notestore with test files
	testFiles := []notestore.NoteFile{
		{
			Path:      "/data/storage/note1.note",
			RelPath:   "note1.note",
			Name:      "note1.note",
			FileType:  notestore.FileTypeNote,
			SizeBytes: 1024,
		},
		{
			Path:      "/data/storage/note2.note",
			RelPath:   "note2.note",
			Name:      "note2.note",
			FileType:  notestore.FileTypeNote,
			SizeBytes: 2048,
		},
	}
	deps.noteStore.files["NOTE/Note"] = testFiles

	resp, err := http.Get(server.URL + "/files")
	if err != nil {
		t.Fatalf("failed to GET /files: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify response contains file data
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	respStr := string(buf)
	// Verify the page contains the file names
	if !strings.Contains(respStr, "note1.note") {
		t.Errorf("expected note1.note in response")
	}
	if !strings.Contains(respStr, "note2.note") {
		t.Errorf("expected note2.note in response")
	}
}

// AC8.2: Job status shows pending/in-flight counts from processor
func TestJobStatus(t *testing.T) {
	server, _ := setupWebTestServer(t, false)

	resp, err := http.Get(server.URL + "/files/status")
	if err != nil {
		t.Fatalf("failed to GET /files/status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Check that response contains JSON with counts
	var result processor.ProcessorStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Pending != 2 || result.InFlight != 1 {
		t.Errorf("unexpected status counts: pending=%d, in_flight=%d", result.Pending, result.InFlight)
	}
}

// AC8.3: FTS5 search returns results
func TestSearch(t *testing.T) {
	server, deps := setupWebTestServer(t, false)

	// Seed mock search index with test results
	searchResults := []search.SearchResult{
		{
			Path:    "/data/storage/meeting.note",
			Snippet: "handwritten meeting notes about project planning",
		},
	}
	deps.searchIndex.results["meeting"] = searchResults

	resp, err := http.Get(server.URL + "/search?q=meeting")
	if err != nil {
		t.Fatalf("failed to GET /search: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify response contains the search snippet
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	respStr := string(buf)
	if !strings.Contains(respStr, "meeting") {
		t.Errorf("expected search result snippet 'meeting' in response")
	}
}

// AC8.4: Task list view shows tasks
func TestTaskList(t *testing.T) {
	server, deps := setupWebTestServer(t, false)

	// Seed mock task store with test tasks
	tasks := []*taskstore.Task{
		{
			TaskID: "task1",
			Title:  taskstore.SqlStr("Buy groceries"),
			Status: taskstore.SqlStr("needsAction"),
		},
		{
			TaskID: "task2",
			Title:  taskstore.SqlStr("Fix bug in handler"),
			Status: taskstore.SqlStr("needsAction"),
		},
		{
			TaskID: "task3",
			Title:  taskstore.SqlStr("Review code"),
			Status: taskstore.SqlStr("completed"),
		},
	}
	for _, task := range tasks {
		deps.store.tasks[task.TaskID] = task
	}

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("failed to GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	respStr := string(buf)
	// Verify response contains the task titles
	if !strings.Contains(respStr, "Buy groceries") {
		t.Errorf("expected task title 'Buy groceries' in response")
	}
	if !strings.Contains(respStr, "Fix bug in handler") {
		t.Errorf("expected task title 'Fix bug in handler' in response")
	}
	if !strings.Contains(respStr, "Review code") {
		t.Errorf("expected task title 'Review code' in response")
	}
}

// Health endpoint should not require authentication
func TestHealthEndpoint(t *testing.T) {
	server, _ := setupWebTestServer(t, true) // Even with auth configured

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatalf("failed to GET /health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	buf, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(buf), "ok") {
		t.Errorf("expected 'ok' in health response")
	}
}

// Auth rejection: missing credentials
func TestAuthRejection(t *testing.T) {
	server, _ := setupWebTestServer(t, true)

	resp, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("failed to GET /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 without auth, got %d", resp.StatusCode)
	}

	// Check for WWW-Authenticate header
	if auth := resp.Header.Get("WWW-Authenticate"); auth == "" {
		t.Errorf("expected WWW-Authenticate header")
	}
}

// Task completion test
func TestTaskCompletion(t *testing.T) {
	server, _ := setupWebTestServer(t, false)

	// Create client that doesn't follow redirects to check redirect status
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Test the endpoint exists
	resp, err := client.Post(server.URL+"/tasks/task1/complete", "text/plain", nil)
	if err != nil {
		t.Fatalf("failed to POST /tasks/task1/complete: %v", err)
	}
	defer resp.Body.Close()

	// Should redirect (303) or return 404 since task doesn't exist
	if resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 303 or 404, got %d", resp.StatusCode)
	}
}

// Processor start/stop test
func TestProcessorControl(t *testing.T) {
	server, _ := setupWebTestServer(t, false)

	// Create client that doesn't follow redirects to properly test redirect status code
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Start processor
	resp, err := client.Post(server.URL+"/processor/start", "text/plain", nil)
	if err != nil {
		t.Fatalf("failed to POST /processor/start: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 for start, got %d", resp.StatusCode)
	}

	// Stop processor
	resp, err = client.Post(server.URL+"/processor/stop", "text/plain", nil)
	if err != nil {
		t.Fatalf("failed to POST /processor/stop: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("expected 303 for stop, got %d", resp.StatusCode)
	}
}

// Verify handlers exist
func TestHandlersExist(t *testing.T) {
	server, _ := setupWebTestServer(t, false)

	endpoints := []string{
		"/",
		"/files",
		"/search",
		"/files/status",
		"/files/history?path=/test.note",
		"/files/content?path=/test.note",
	}

	for _, ep := range endpoints {
		resp, err := http.Get(server.URL + ep)
		if err != nil {
			t.Fatalf("failed to GET %s: %v", ep, err)
		}
		resp.Body.Close()

		// Just verify we got a response (not 404)
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("endpoint %s returned 404", ep)
		}
	}
}
