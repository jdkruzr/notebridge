package sync

import (
	"path/filepath"
	"strconv"
)

// AutoRename generates a non-colliding filename by appending (N) before the extension.
// Given a filename like "notes.pdf" and existing names at the destination,
// generates "notes(1).pdf", "notes(2).pdf", etc. until a non-colliding name is found.
// For folders (no extension), appends "(N)" directly.
func AutoRename(baseName string, existingNames []string) string {
	// Build a set of existing names for fast lookup
	existing := make(map[string]bool)
	for _, name := range existingNames {
		existing[name] = true
	}

	// If baseName doesn't collide, return it
	if !existing[baseName] {
		return baseName
	}

	// Split name and extension
	name, ext := SplitNameExt(baseName)

	// Try (1), (2), (3), ... until we find a non-colliding name
	for i := 1; i <= 1000; i++ {
		candidate := name + "(" + strconv.Itoa(i) + ")" + ext
		if !existing[candidate] {
			return candidate
		}
	}

	// Fallback (shouldn't happen with reasonable inputs)
	return name + "(1000)" + ext
}

// IsCircularMove checks if movingID appears in the ancestor chain.
// Returns true if the move would create a cycle, false if safe.
func IsCircularMove(movingID int64, ancestorIDs []int64) bool {
	for _, ancestorID := range ancestorIDs {
		if ancestorID == movingID {
			return true
		}
	}
	return false
}

// SplitNameExt splits a filename into name and extension.
// Examples:
//   "notes.pdf" -> ("notes", ".pdf")
//   "archive.tar.gz" -> ("archive.tar", ".gz")
//   "README" -> ("README", "")
func SplitNameExt(filename string) (name, ext string) {
	ext = filepath.Ext(filename)
	name = filename[:len(filename)-len(ext)]
	return name, ext
}
