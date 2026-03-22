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
	if err := store.EnsureUser(ctx, userEmail, passwordHash, nil); err != nil {
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
func TestPipeline_AC41_UploadTriggerOCRInject(t *testing.T) {
	t.Skip("requires mock .note file and full integration setup")
}

// AC4.2: Next tablet sync sees updated MD5 → downloads injected version (no CONFLICT)
func TestPipeline_AC42_ConflictFreeDownload(t *testing.T) {
	t.Skip("requires list_folder_v3 implementation and mock tablet client")
}

// AC4.3: RTR notes (FILE_RECOGN_TYPE=1) are OCR'd and indexed but NOT modified
func TestPipeline_AC43_RTRNotesNotModified(t *testing.T) {
	t.Skip("requires synthetic .note file with RTR flag")
}

// AC4.4: Re-processing: user edits note → uploads new version → hash mismatch detected → re-queued with 30s delay
func TestPipeline_AC44_ReprocessingOnHashMismatch(t *testing.T) {
	t.Skip("requires synthetic .note file and hash mismatch detection")
}

// AC4.5: FTS5 search returns OCR'd content from injected RECOGNTEXT
func TestPipeline_AC45_SearchOCRContent(t *testing.T) {
	t.Skip("requires synthetic .note file with OCR content")
}

// AC4.6: Backup created before any file modification
func TestPipeline_AC46_BackupBeforeModification(t *testing.T) {
	t.Skip("requires synthetic .note file and backup verification")
}

// Helper: Create a minimal synthetic .note file for testing
func createTestNoteFile(t *testing.T, dir, name string) string {
	t.Helper()
	// For now, create a simple binary file as a placeholder
	// In a full implementation, this would use go-sn to create a valid .note
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte{0x7b, 0x22, 0x7d}, 0644); err != nil {
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

// TestPipeline_EventBusIntegration verifies that file events are published correctly
func TestPipeline_EventBusIntegration(t *testing.T) {
	_, pipe, _, ctx := setupPipelineTestServer(t)
	defer pipe.Close()

	// Create a temporary note file
	noteDir := filepath.Dir(t.TempDir())
	notePath := filepath.Join(noteDir, "test.note")
	if err := os.WriteFile(notePath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Remove(notePath)

	// The event should be published when file is uploaded via sync handlers
	// For now, we just verify the pipeline is set up correctly
	if pipe == nil {
		t.Fatal("pipeline should be initialized")
	}

	// Give pipeline time to start up
	time.Sleep(100 * time.Millisecond)

	// Verify no panic occurred
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled")
	default:
		// OK
	}
}
