package blob

import (
	"context"
	"io"
)

// BlobStore is an interface for managing file storage.
// Keys are relative paths under the storage root (e.g., user@email.com/Supernote/Note/file.note).
type BlobStore interface {
	// Put writes data from r to the given key, returns size and MD5 hex.
	Put(ctx context.Context, key string, r io.Reader) (size int64, md5hex string, err error)

	// Get opens and reads from the given key, returns file handle and size.
	// Returns an os.ErrNotExist-based error if the key doesn't exist.
	Get(ctx context.Context, key string) (io.ReadCloser, int64, error)

	// Delete removes the file at the given key.
	// Does not error if the key doesn't exist.
	Delete(ctx context.Context, key string) error

	// Exists checks if a file exists at the given key.
	Exists(ctx context.Context, key string) bool

	// Path returns the absolute filesystem path for a key.
	// Used for http.ServeContent (Range support) and pipeline access.
	Path(key string) string
}
