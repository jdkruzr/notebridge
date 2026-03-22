package events

// Event represents a file system event that occurred.
type Event struct {
	Type   string // event type constant
	FileID int64
	UserID int64
	Path   string
}

// Event type constants
const (
	FileUploaded  = "file.uploaded"
	FileModified  = "file.modified"
	FileDeleted   = "file.deleted"
)
