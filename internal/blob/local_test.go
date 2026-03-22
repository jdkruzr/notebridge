package blob

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestPutWritesFileAndReturnsSizeAndMD5(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	testData := []byte("hello world")
	expectedMD5 := fmt.Sprintf("%x", md5.Sum(testData))

	size, md5hex, err := store.Put(ctx, "test/file.txt", bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if size != int64(len(testData)) {
		t.Errorf("expected size %d, got %d", len(testData), size)
	}

	if md5hex != expectedMD5 {
		t.Errorf("expected md5 %s, got %s", expectedMD5, md5hex)
	}
}

func TestPutCreatesIntermediateDirectories(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	testData := []byte("test content")
	_, _, err := store.Put(ctx, "a/b/c/d/file.txt", bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("Put with nested path failed: %v", err)
	}

	// Verify the file exists at the correct path
	path := store.Path("a/b/c/d/file.txt")
	_, err = os.Stat(path)
	if err != nil {
		t.Errorf("file not created at expected path: %v", err)
	}
}

func TestGetReturnsCorrectContentAndSize(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	testData := []byte("test file content")
	_, _, err := store.Put(ctx, "test.txt", bytes.NewReader(testData))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	reader, size, err := store.Get(ctx, "test.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer reader.Close()

	if size != int64(len(testData)) {
		t.Errorf("expected size %d, got %d", len(testData), size)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read content: %v", err)
	}

	if !bytes.Equal(content, testData) {
		t.Errorf("expected content %v, got %v", testData, content)
	}
}

func TestGetNonExistentKeyReturnsError(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	_, _, err := store.Get(ctx, "nonexistent.txt")
	if err == nil {
		t.Fatal("expected error for non-existent key")
	}

	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got %T: %v", err, err)
	}
}

func TestDeleteRemovesFile(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	// Put a file
	_, _, err := store.Put(ctx, "test.txt", bytes.NewReader([]byte("content")))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Verify it exists
	if !store.Exists(ctx, "test.txt") {
		t.Fatal("file should exist after Put")
	}

	// Delete it
	err = store.Delete(ctx, "test.txt")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it no longer exists
	if store.Exists(ctx, "test.txt") {
		t.Fatal("file should not exist after Delete")
	}
}

func TestDeleteNonExistentKeyDoesNotError(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	err := store.Delete(ctx, "nonexistent.txt")
	if err != nil {
		t.Errorf("expected no error for deleting non-existent file, got %v", err)
	}
}

func TestExistsReturnsTrueForExistingKey(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	_, _, err := store.Put(ctx, "existing.txt", bytes.NewReader([]byte("content")))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if !store.Exists(ctx, "existing.txt") {
		t.Fatal("Exists should return true for existing key")
	}
}

func TestExistsReturnsFalseForMissingKey(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	if store.Exists(ctx, "nonexistent.txt") {
		t.Fatal("Exists should return false for non-existent key")
	}
}

func TestPathReturnsCorrectAbsolutePath(t *testing.T) {
	rootDir := t.TempDir()
	store := NewLocalStore(rootDir)

	expected := filepath.Join(rootDir, "user@example.com/files/note.pdf")
	actual := store.Path("user@example.com/files/note.pdf")

	if actual != expected {
		t.Errorf("expected path %s, got %s", expected, actual)
	}
}

func TestPutWithEmptyReaderReturnsZeroSize(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	size, md5hex, err := store.Put(ctx, "empty.txt", bytes.NewReader([]byte{}))
	if err != nil {
		t.Fatalf("Put with empty reader failed: %v", err)
	}

	if size != 0 {
		t.Errorf("expected size 0 for empty file, got %d", size)
	}

	// MD5 of empty string
	expectedMD5 := "d41d8cd98f00b204e9800998ecf8427e"
	if md5hex != expectedMD5 {
		t.Errorf("expected md5 %s for empty content, got %s", expectedMD5, md5hex)
	}
}

func TestConcurrentPutToDifferentKeysSucceeds(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ctx := context.Background()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := fmt.Sprintf("file_%d.txt", idx)
			data := []byte(fmt.Sprintf("content_%d", idx))
			_, _, err := store.Put(ctx, key, bytes.NewReader(data))
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", idx, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent Put failed: %v", err)
	}

	// Verify all files were created
	for i := 0; i < numGoroutines; i++ {
		key := fmt.Sprintf("file_%d.txt", i)
		if !store.Exists(ctx, key) {
			t.Errorf("file %s was not created", key)
		}
	}
}
