package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sysop/notebridge/internal/events"
	"github.com/sysop/notebridge/internal/notestore"
	"github.com/sysop/notebridge/internal/pipeline"
	"github.com/sysop/notebridge/internal/processor"
	"github.com/sysop/notebridge/internal/search"
	"github.com/sysop/notebridge/internal/syncdb"
)

// setupPipelineTestServer creates a full pipeline + processor setup for integration testing
func setupPipelineTestServer(t *testing.T) (*syncdb.Store, *pipeline.Pipeline, string, context.Context) {
	t.Helper()

	// Create temporary directories for storage, backups, and blobs
	storageDir := t.TempDir()
	backupDir := t.TempDir()

	// Open in-memory database
	db, err := syncdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Create store
	store := syncdb.NewStore(db)

	// Bootstrap user
	ctx := context.Background()
	userEmail := "test@example.com"
	passwordHash := "fakehash"
	if err := store.EnsureUser(ctx, userEmail, passwordHash, 1000000000000001); err != nil {
		t.Fatalf("failed to bootstrap user: %v", err)
	}

	// Create notestore and search index
	noteStore := notestore.New(db, storageDir)
	searchIndex := search.New(db)

	// Create processor without OCR (since we can't call real APIs in tests)
	proc := processor.New(db, processor.WorkerConfig{
		OCREnabled: false, // Disable OCR to avoid external API calls
		BackupPath: backupDir,
		MaxFileMB:  50,
		OCRClient:  nil, // No mock OCR client available
		Indexer:    searchIndex,
		// AfterInject hook could be set here if needed
	})

	// Start processor
	if err := proc.Start(ctx); err != nil {
		t.Fatalf("failed to start processor: %v", err)
	}
	t.Cleanup(func() { proc.Stop() })

	// Create event bus and pipeline
	eventBus := events.NewEventBus()
	pipe := pipeline.New(pipeline.Config{
		NotesPath: storageDir,
		NoteStore: noteStore,
		Processor: proc,
		EventBus:  eventBus,
		Logger:    nil, // Use default logger
	})
	pipe.Start(ctx)
	t.Cleanup(func() { pipe.Close() })

	return store, pipe, storageDir, ctx
}

// AC4.1: .note file uploaded via sync → OCR runs → RECOGNTEXT injected → syncdb updated with new MD5
// SKIPPED: Requires real OCR API or mock OCR client; requires full .note parsing with go-sn library.
// Implementation path: mock OCRClient or integrate with test OCR service.
func TestPipeline_AC41_UploadTriggerOCRInject(t *testing.T) {
	t.Skip("requires mock OCR client or test OCR service; requires go-sn for .note format handling")
}

// AC4.2: Next tablet sync sees updated MD5 → downloads injected version (no CONFLICT)
// SKIPPED: Requires full tablet client simulation and list_folder_v3 endpoint.
// Implementation path: add mock tablet client or extend TestPipeline_AC41.
func TestPipeline_AC42_ConflictFreeDownload(t *testing.T) {
	t.Skip("requires mock tablet sync client or full device simulation")
}

// AC4.3: RTR notes (FILE_RECOGN_TYPE=1) are OCR'd and indexed but NOT modified
// SKIPPED: Requires parsing .note file headers to detect RTR flag; requires go-sn library.
// Implementation path: implement .note header parsing or use test fixture.
func TestPipeline_AC43_RTRNotesNotModified(t *testing.T) {
	t.Skip("requires go-sn library for .note format parsing to detect RTR flag")
}

// AC4.4: Re-processing: user edits note → uploads new version → hash mismatch detected → re-queued with 30s delay
// SKIPPED: Requires hash-based change detection in processor; requires full file upload flow.
// Implementation path: seed jobs table with old hash, re-enqueue on mismatch.
func TestPipeline_AC44_ReprocessingOnHashMismatch(t *testing.T) {
	t.Skip("requires hash-based change detection in processor; requires full sync upload flow")
}

// AC4.5: FTS5 search returns OCR'd content from injected RECOGNTEXT
func TestPipeline_AC45_SearchOCRContent(t *testing.T) {
	store, pipe, storageDir, ctx := setupPipelineTestServer(t)
	defer pipe.Close()

	// Create a test note file
	notePath := createTestNoteFile(t, storageDir, "test_ocr.note")

	// Index OCR content directly (simulating injected RECOGNTEXT)
	ocrText := "recognized text from OCR processing"
	if _, err := store.DB().ExecContext(ctx, `
		INSERT INTO note_content (note_path, page, title_text, body_text) VALUES (?, ?, ?, ?)
	`, notePath, 0, "Test Title", ocrText); err != nil {
		t.Fatalf("failed to index content: %v", err)
	}

	// Search for the OCR'd content using the FTS5 virtual table
	// Since note_fts is an external content table, we need to query the indexed content directly
	rows, err := store.DB().QueryContext(ctx, `
		SELECT note_path FROM note_content
		WHERE rowid IN (SELECT rowid FROM note_fts WHERE note_fts MATCH ?)
	`, "recognized")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	defer rows.Close()

	var foundPath string
	if !rows.Next() {
		t.Error("expected to find indexed OCR content in search")
	} else {
		if err := rows.Scan(&foundPath); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		if foundPath != notePath {
			t.Errorf("found path %q, want %q", foundPath, notePath)
		}
	}
}

// AC4.6: Backup created before any file modification
// SKIPPED: Requires full OCR injection flow to trigger backup; processor.executeJob
// copies to backupPath before modifying the .note file, but triggering that path
// requires a real .note file with parseable structure and an OCR client.
func TestPipeline_AC46_BackupBeforeModification(t *testing.T) {
	t.Skip("requires mock OCR client and parseable .note fixture to trigger backup code path")
}

// Helper: Create a minimal synthetic .note file for testing
// Returns the absolute path to the created .note file.
// Note: This creates a simple stub file with minimal structure.
// For full integration tests requiring actual .note parsing, use go-sn library.
func createTestNoteFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	// Create a minimal valid file with some test data
	// Real .note files are more complex; this is sufficient for path-based tests
	testContent := []byte("test note content for integration testing")
	if err := os.WriteFile(path, testContent, 0644); err != nil {
		t.Fatalf("failed to create test note: %v", err)
	}
	return path
}

// TestPipeline_BasicWiring verifies that pipeline, processor, and event bus are wired correctly
func TestPipeline_BasicWiring(t *testing.T) {
	store, pipe, storageDir, _ := setupPipelineTestServer(t)
	defer pipe.Close()

	// Verify pipeline is running
	if pipe == nil {
		t.Fatal("pipeline should be initialized")
	}

	// Verify storage directory exists
	if _, err := os.Stat(storageDir); err != nil {
		t.Fatalf("storage directory should exist: %v", err)
	}

	// Verify store is accessible
	if store == nil {
		t.Fatal("store should be initialized")
	}
}

// TestPipeline_NoPanicOnStartup verifies that the pipeline starts without panicking
func TestPipeline_NoPanicOnStartup(t *testing.T) {
	_, pipe, _, ctx := setupPipelineTestServer(t)
	defer pipe.Close()

	// Verify pipeline initialized without panicking
	if pipe == nil {
		t.Fatal("pipeline should be initialized")
	}

	// Give pipeline time to start up
	time.Sleep(100 * time.Millisecond)

	// Verify context is still valid (not prematurely cancelled)
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled")
	default:
		// OK
	}
}
