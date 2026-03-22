package blob

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStore implements BlobStore using the local filesystem.
type LocalStore struct {
	rootDir string
}

// NewLocalStore creates a new LocalStore with the given root directory.
// The root directory is created if it doesn't exist.
func NewLocalStore(rootDir string) *LocalStore {
	return &LocalStore{
		rootDir: rootDir,
	}
}

// Put writes data from r to a file at the key, computing MD5.
// Creates intermediate directories as needed, uses a temp file, and atomically renames.
func (l *LocalStore) Put(ctx context.Context, key string, r io.Reader) (int64, string, error) {
	// Construct the final path
	finalPath := filepath.Join(l.rootDir, key)

	// Validate resolved path is within rootDir (prevent path traversal)
	absRootDir, err := filepath.Abs(l.rootDir)
	if err != nil {
		return 0, "", fmt.Errorf("failed to resolve root directory: %w", err)
	}
	absFinalPath, err := filepath.Abs(finalPath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to resolve final path: %w", err)
	}
	if !strings.HasPrefix(absFinalPath, absRootDir+string(os.PathSeparator)) && absFinalPath != absRootDir {
		return 0, "", fmt.Errorf("path traversal attempt: %s outside root %s", absFinalPath, absRootDir)
	}

	// Create parent directories
	if err := os.MkdirAll(filepath.Dir(finalPath), 0755); err != nil {
		return 0, "", fmt.Errorf("failed to create directories: %w", err)
	}

	// Create temp file in the same directory (for atomic rename)
	dir := filepath.Dir(finalPath)
	tempFile, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return 0, "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name()) // Clean up temp file if something goes wrong

	// Compute MD5 while writing (using TeeReader)
	md5Hash := md5.New()
	teeReader := io.TeeReader(r, md5Hash)

	// Write to temp file
	size, err := io.Copy(tempFile, teeReader)
	if err != nil {
		return 0, "", fmt.Errorf("failed to write file: %w", err)
	}

	tempFile.Close()

	// Atomically rename temp file to final path
	if err := os.Rename(tempFile.Name(), finalPath); err != nil {
		return 0, "", fmt.Errorf("failed to rename file: %w", err)
	}

	// Return size and MD5 hex
	md5hex := fmt.Sprintf("%x", md5Hash.Sum(nil))
	return size, md5hex, nil
}

// Get opens a file for reading and returns its size.
func (l *LocalStore) Get(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	path := filepath.Join(l.rootDir, key)

	// Validate resolved path is within rootDir (prevent path traversal)
	absRootDir, err := filepath.Abs(l.rootDir)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve root directory: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to resolve path: %w", err)
	}
	if !strings.HasPrefix(absPath, absRootDir+string(os.PathSeparator)) && absPath != absRootDir {
		return nil, 0, fmt.Errorf("path traversal attempt: %s outside root %s", absPath, absRootDir)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}

	// Stat the file to get size
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, err
	}

	return file, info.Size(), nil
}

// Delete removes a file at the given key.
// Returns no error if the file doesn't exist.
func (l *LocalStore) Delete(ctx context.Context, key string) error {
	path := filepath.Join(l.rootDir, key)
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Ignore "not found" errors
	}
	return err
}

// Exists checks if a file exists at the given key.
func (l *LocalStore) Exists(ctx context.Context, key string) bool {
	path := filepath.Join(l.rootDir, key)
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// Path returns the absolute filesystem path for a key.
func (l *LocalStore) Path(key string) string {
	return filepath.Join(l.rootDir, key)
}
