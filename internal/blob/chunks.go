package blob

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ChunkStore handles temporary storage of chunked uploads and merging them into final files.
type ChunkStore struct {
	rootDir string
}

// NewChunkStore creates a new ChunkStore with the given root directory.
func NewChunkStore(rootDir string) *ChunkStore {
	return &ChunkStore{
		rootDir: rootDir,
	}
}

// SaveChunk saves a chunk to disk and returns its MD5 hash.
// The chunk is stored at {rootDir}/{uploadID}/part_{partNumber:05d}.
func (cs *ChunkStore) SaveChunk(uploadID string, partNumber int, r io.Reader) (string, error) {
	// Create upload directory
	uploadDir := filepath.Join(cs.rootDir, uploadID)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create upload directory: %w", err)
	}

	// Create chunk file path
	chunkPath := filepath.Join(uploadDir, fmt.Sprintf("part_%05d", partNumber))

	// Create temp file in the same directory
	tempFile, err := os.CreateTemp(uploadDir, ".tmp-")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())

	// Compute MD5 while writing
	md5Hash := md5.New()
	teeReader := io.TeeReader(r, md5Hash)

	// Write to temp file
	_, err = io.Copy(tempFile, teeReader)
	if err != nil {
		return "", fmt.Errorf("failed to write chunk: %w", err)
	}

	tempFile.Close()

	// Atomically rename temp file to final path
	if err := os.Rename(tempFile.Name(), chunkPath); err != nil {
		return "", fmt.Errorf("failed to rename chunk file: %w", err)
	}

	md5hex := fmt.Sprintf("%x", md5Hash.Sum(nil))
	return md5hex, nil
}

// MergeChunks reads all chunk parts in order (1..totalChunks), streams through destStore.Put,
// and returns the final file size and MD5. Cleans up chunk directory after successful merge.
func (cs *ChunkStore) MergeChunks(uploadID string, totalChunks int, destStore BlobStore, destKey string) (int64, string, error) {
	uploadDir := filepath.Join(cs.rootDir, uploadID)

	// Create a pipe to stream chunks through
	pr, pw := io.Pipe()
	defer pr.Close()

	// Run the merge in a goroutine to avoid deadlock
	var mergeErr error
	go func() {
		defer pw.Close()
		for i := 1; i <= totalChunks; i++ {
			chunkPath := filepath.Join(uploadDir, fmt.Sprintf("part_%05d", i))
			chunk, err := os.Open(chunkPath)
			if err != nil {
				mergeErr = fmt.Errorf("failed to open chunk %d: %w", i, err)
				return
			}
			_, err = io.Copy(pw, chunk)
			chunk.Close()
			if err != nil {
				mergeErr = fmt.Errorf("failed to copy chunk %d: %w", i, err)
				return
			}
		}
	}()

	// Stream through destStore.Put (which computes final MD5)
	size, md5hex, err := destStore.Put(nil, destKey, pr)
	if err != nil {
		return 0, "", fmt.Errorf("failed to merge chunks into destination: %w", err)
	}

	if mergeErr != nil {
		return 0, "", mergeErr
	}

	// Clean up chunk directory
	if err := os.RemoveAll(uploadDir); err != nil {
		return 0, "", fmt.Errorf("failed to clean up chunk directory: %w", err)
	}

	return size, md5hex, nil
}

// Cleanup removes the entire upload directory.
func (cs *ChunkStore) Cleanup(uploadID string) error {
	uploadDir := filepath.Join(cs.rootDir, uploadID)
	if err := os.RemoveAll(uploadDir); err != nil {
		return fmt.Errorf("failed to cleanup upload directory: %w", err)
	}
	return nil
}
