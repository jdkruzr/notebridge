package syncdb

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"
)

var (
	ErrSyncLocked            = errors.New("sync locked by another device")
	ErrTaskGroupNotFound     = errors.New("task group not found")
	ErrTaskNotFound          = errors.New("task not found")
	ErrSummaryGroupNotFound  = errors.New("summary group not found")
	ErrSummaryNotFound       = errors.New("summary not found")
	ErrUniqueIDExists        = errors.New("summary unique ID already exists")
)

// Allowed field names for partial updates to prevent SQL injection
var (
	// Schedule group fields
	scheduleGroupAllowedFields = map[string]bool{
		"title":           true,
		"last_modified":   true,
		"create_time":     true,
	}

	// Schedule task fields
	scheduleTaskAllowedFields = map[string]bool{
		"title":               true,
		"detail":              true,
		"status":              true,
		"importance":          true,
		"recurrence":          true,
		"links":               true,
		"is_reminder_on":      true,
		"due_time":            true,
		"completed_time":      true,
		"last_modified":       true,
		"sort":                true,
		"sort_completed":      true,
		"planer_sort":         true,
		"sort_time":           true,
		"planer_sort_time":    true,
		"all_sort":            true,
		"all_sort_completed":  true,
		"all_sort_time":       true,
		"recurrence_id":       true,
	}

	// Summary fields
	summaryAllowedFields = map[string]bool{
		"name":                      true,
		"description":               true,
		"md5_hash":                  true,
		"handwrite_md5":             true,
		"handwrite_inner_name":      true,
		"metadata":                  true,
		"content":                   true,
		"data_source":               true,
		"source_path":               true,
		"source_type":               true,
		"tags":                      true,
		"file_id":                   true,
		"parent_unique_identifier":  true,
		"comment_fields":            true,
		"handwrite_fields":          true,
		"comment_handwrite_name":    true,
		"author":                    true,
		"creation_time":             true,
		"last_modified_time":        true,
	}
)

// validateAndFilterFields validates field names against allowlist and returns filtered updates
func validateAndFilterFields(updates map[string]interface{}, allowedFields map[string]bool) (map[string]interface{}, error) {
	filtered := make(map[string]interface{})
	for key, value := range updates {
		if !allowedFields[key] {
			return nil, fmt.Errorf("invalid field name: %s", key)
		}
		filtered[key] = value
	}
	return filtered, nil
}

// Store is the data access layer for auth operations.
type Store struct {
	db *sql.DB
}

// User represents a user record.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	ErrorCount   int
	LastErrorAt  *time.Time
	LockedUntil  *time.Time
}

// AuthToken represents an authentication token record.
type AuthToken struct {
	Key        string
	Token      string
	UserID     int64
	EquipmentNo string
	ExpiresAt  time.Time
}

// FileEntry represents a file or folder in the catalog.
type FileEntry struct {
	ID          int64
	UserID      int64
	DirectoryID int64
	FileName    string
	InnerName   string
	StorageKey  string
	MD5         string
	Size        int64
	IsFolder    bool
	IsActive    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ScheduleGroup represents a task list/group.
type ScheduleGroup struct {
	TaskListID   string
	UserID       int64
	Title        string
	LastModified int64
	CreateTime   int64
}

// ScheduleTask represents a single task.
type ScheduleTask struct {
	TaskID           string `json:"taskId"`
	UserID           int64  `json:"userId"`
	TaskListID       string `json:"taskListId"`
	Title            string `json:"title"`
	Detail           string `json:"detail"`
	Status           string `json:"status"`
	Importance       string `json:"importance"`
	Recurrence       string `json:"recurrence"`
	Links            string `json:"links"`
	IsReminderOn     string `json:"isReminderOn"`
	DueTime          int64  `json:"dueTime"`
	CompletedTime    int64  `json:"completedTime"`
	LastModified     int64  `json:"lastModified"`
	Sort             int64  `json:"sort"`
	SortCompleted    int64  `json:"sortCompleted"`
	PlanerSort       int64  `json:"planerSort"`
	SortTime         int64  `json:"sortTime"`
	PlanerSortTime   int64  `json:"planerSortTime"`
	AllSort          int64  `json:"allSort"`
	AllSortCompleted int64  `json:"allSortCompleted"`
	AllSortTime      int64  `json:"allSortTime"`
	RecurrenceID     string `json:"recurrenceId"`
}

// TaskUpdate represents partial updates for a task.
type TaskUpdate struct {
	TaskID string
	Fields map[string]interface{}
}

// Summary represents a digest/summary item.
type Summary struct {
	ID                      int64  `json:"id"`
	UserID                  int64  `json:"userId"`
	UniqueIdentifier        string `json:"uniqueIdentifier"`
	Name                    string `json:"name"`
	Description             string `json:"description"`
	FileID                  int64  `json:"fileId"`
	ParentUniqueIdentifier  string `json:"parentUniqueIdentifier"`
	Content                 string `json:"content"`
	DataSource              string `json:"dataSource"`
	SourcePath              string `json:"sourcePath"`
	SourceType              int64  `json:"sourceType"`
	Tags                    string `json:"tags"`
	MD5Hash                 string `json:"md5Hash"`
	HandwriteMD5            string `json:"handwriteMD5"`
	HandwriteInnerName      string `json:"handwriteInnerName"`
	Metadata                string `json:"metadata"`
	CommentStr              string `json:"commentStr"`
	HandwriteFields         string `json:"-"` // DB column exists but not in SPC protocol
	CommentHandwriteName    string `json:"commentHandwriteName"`
	IsSummaryGroup          string `json:"isSummaryGroup"`
	IsDeleted               string `json:"isDeleted"`
	Author                  string `json:"author"`
	CreationTime            int64  `json:"creationTime"`
	LastModifiedTime        int64  `json:"lastModifiedTime"`
}

// SummaryHash represents lightweight summary hash data.
type SummaryHash struct {
	ID                   int64  `json:"id"`
	MD5Hash              string `json:"md5Hash"`
	HandwriteMD5         string `json:"handwriteMD5"`
	CommentHandwriteName string `json:"commentHandwriteName"`
	LastModifiedTime     int64  `json:"lastModifiedTime"`
	Metadata             string `json:"metadata"`
}

// NewStore creates a new Store instance.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// EnsureUser inserts a user if it doesn't exist (INSERT OR IGNORE).
// If snowflake is provided, it's used to generate the user ID; otherwise SQLite auto-increments.
func (s *Store) EnsureUser(ctx context.Context, email, passwordHash string, snowflakeID int64) error {
	query := `
		INSERT OR IGNORE INTO users (id, email, password_hash, error_count, last_error_at, locked_until)
		VALUES (?, ?, ?, 0, NULL, NULL)
	`

	_, err := s.db.ExecContext(ctx, query, snowflakeID, email, passwordHash)
	return err
}

// GetUserByID retrieves a user by ID. Returns nil if not found (not an error).
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	query := `
		SELECT id, email, password_hash, COALESCE(error_count, 0), last_error_at, locked_until
		FROM users
		WHERE id = ?
	`
	var user User
	var lastErrorAtStr sql.NullString
	var lockedUntilStr sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.ErrorCount,
		&lastErrorAtStr, &lockedUntilStr,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail retrieves a user by email. Returns nil if not found (not an error).
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, COALESCE(error_count, 0), last_error_at, locked_until
		FROM users
		WHERE email = ?
	`

	var user User
	var lastErrorAtStr sql.NullString
	var lockedUntilStr sql.NullString

	err := s.db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.ErrorCount,
		&lastErrorAtStr,
		&lockedUntilStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found, not an error
	}
	if err != nil {
		return nil, err
	}

	// Parse timestamp fields
	if lastErrorAtStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, lastErrorAtStr.String)
		if err == nil {
			user.LastErrorAt = &t
		}
	}

	if lockedUntilStr.Valid {
		t, err := time.Parse(time.RFC3339Nano, lockedUntilStr.String)
		if err == nil {
			user.LockedUntil = &t
		}
	}

	return &user, nil
}

// IncrementErrorCount bumps the error count and sets last_error_at to now.
func (s *Store) IncrementErrorCount(ctx context.Context, userID int64) error {
	query := `
		UPDATE users
		SET error_count = COALESCE(error_count, 0) + 1,
		    last_error_at = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, time.Now().Format(time.RFC3339Nano), userID)
	return err
}

// ResetErrorCount sets error_count to 0.
func (s *Store) ResetErrorCount(ctx context.Context, userID int64) error {
	query := `
		UPDATE users
		SET error_count = 0
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, userID)
	return err
}

// LockUser sets locked_until to the given time.
func (s *Store) LockUser(ctx context.Context, userID int64, until time.Time) error {
	query := `
		UPDATE users
		SET locked_until = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, until.Format(time.RFC3339Nano), userID)
	return err
}

// CreateChallenge inserts a login challenge.
func (s *Store) CreateChallenge(ctx context.Context, account, code string, timestamp int64) error {
	query := `
		INSERT INTO login_challenges (account, timestamp, random_code)
		VALUES (?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, account, timestamp, code)
	return err
}

// GetChallenge retrieves the random code for a challenge.
func (s *Store) GetChallenge(ctx context.Context, account string, timestamp int64) (string, error) {
	query := `
		SELECT random_code
		FROM login_challenges
		WHERE account = ? AND timestamp = ?
	`

	var code string
	err := s.db.QueryRowContext(ctx, query, account, timestamp).Scan(&code)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", errors.New("challenge not found")
		}
		return "", err
	}

	return code, nil
}

// DeleteChallenge removes a challenge.
func (s *Store) DeleteChallenge(ctx context.Context, account string, timestamp int64) error {
	query := `
		DELETE FROM login_challenges
		WHERE account = ? AND timestamp = ?
	`

	_, err := s.db.ExecContext(ctx, query, account, timestamp)
	return err
}

// StoreToken inserts an auth token.
func (s *Store) StoreToken(ctx context.Context, key, token string, userID int64, equipmentNo string, expiresAt time.Time) error {
	query := `
		INSERT INTO auth_tokens (key, token, user_id, equipment_no, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, key, token, userID, equipmentNo, expiresAt.Format(time.RFC3339Nano))
	return err
}

// GetToken retrieves a token by key. Returns nil if not found or expired.
func (s *Store) GetToken(ctx context.Context, key string) (*AuthToken, error) {
	query := `
		SELECT key, token, user_id, equipment_no, expires_at
		FROM auth_tokens
		WHERE key = ? AND expires_at > ?
	`

	var token AuthToken
	var expiresAtStr string

	err := s.db.QueryRowContext(ctx, query, key, time.Now().Format(time.RFC3339Nano)).Scan(
		&token.Key,
		&token.Token,
		&token.UserID,
		&token.EquipmentNo,
		&expiresAtStr,
	)

	if err == sql.ErrNoRows {
		return nil, errors.New("token not found or expired")
	}
	if err != nil {
		return nil, err
	}

	// Parse expiry time
	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token expiry: %w", err)
	}
	token.ExpiresAt = expiresAt

	return &token, nil
}

// DeleteToken revokes a token.
func (s *Store) DeleteToken(ctx context.Context, key string) error {
	query := `
		DELETE FROM auth_tokens
		WHERE key = ?
	`

	_, err := s.db.ExecContext(ctx, query, key)
	return err
}

// GetOrCreateJWTSecret reads the JWT secret from server_settings, or generates a new one if missing.
func (s *Store) GetOrCreateJWTSecret(ctx context.Context) (string, error) {
	// Try to read existing secret
	query := `
		SELECT value
		FROM server_settings
		WHERE key = 'jwt_secret'
	`

	var secret string
	err := s.db.QueryRowContext(ctx, query).Scan(&secret)

	if err == nil {
		return secret, nil // Found existing secret
	}

	if err != sql.ErrNoRows {
		return "", err // Real error
	}

	// Generate new secret (32-byte random as hex = 64 chars)
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}

	secret = hex.EncodeToString(randomBytes)

	// Store it
	insertQuery := `
		INSERT INTO server_settings (key, value)
		VALUES ('jwt_secret', ?)
	`

	_, err = s.db.ExecContext(ctx, insertQuery, secret)
	if err != nil {
		return "", fmt.Errorf("failed to store jwt secret: %w", err)
	}

	return secret, nil
}

// GetOrCreateMachineID reads the machine ID from server_settings, or generates a new one.
func (s *Store) GetOrCreateMachineID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM server_settings WHERE key = 'machine_id'`).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	// Generate new machine ID (32 random bytes, base64url, no padding)
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate machine ID: %w", err)
	}
	id = hex.EncodeToString(randomBytes)[:32]

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO server_settings (key, value) VALUES ('machine_id', ?)`, id)
	if err != nil {
		return "", fmt.Errorf("failed to store machine ID: %w", err)
	}
	return id, nil
}

// SetMachineID stores a machine ID (e.g., from SPC migration).
func (s *Store) SetMachineID(ctx context.Context, machineID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO server_settings (key, value) VALUES ('machine_id', ?)`, machineID)
	return err
}

// EnsureEquipment inserts equipment if it doesn't exist (INSERT OR IGNORE).
func (s *Store) EnsureEquipment(ctx context.Context, equipmentNo string, userID int64) error {
	query := `
		INSERT OR IGNORE INTO equipment (equipment_no, user_id, status)
		VALUES (?, ?, 'active')
	`

	_, err := s.db.ExecContext(ctx, query, equipmentNo, userID)
	return err
}

// StoreNonce inserts a URL nonce with expiry.
func (s *Store) StoreNonce(ctx context.Context, nonce string, expiresAt time.Time) error {
	query := `
		INSERT INTO url_nonces (nonce, expires_at)
		VALUES (?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, nonce, expiresAt.Format(time.RFC3339Nano))
	return err
}

// ConsumeNonce checks if nonce exists and is not expired, then deletes it.
// Returns true if successfully consumed, false if not found or expired.
func (s *Store) ConsumeNonce(ctx context.Context, nonce string) (bool, error) {
	query := `
		DELETE FROM url_nonces
		WHERE nonce = ? AND expires_at > ?
	`

	result, err := s.db.ExecContext(ctx, query, nonce, time.Now().Format(time.RFC3339Nano))
	if err != nil {
		return false, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rowsAffected > 0, nil
}

// CleanupExpired deletes expired challenges, tokens, and nonces.
func (s *Store) CleanupExpired(ctx context.Context) error {
	now := time.Now()
	nowStr := now.Format(time.RFC3339Nano)
	fiveMinutesAgo := now.Add(-5 * time.Minute).Unix()

	// Delete expired challenges (older than 5 minutes)
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM login_challenges
		WHERE timestamp < ?
	`, fiveMinutesAgo)
	if err != nil {
		return err
	}

	// Delete expired tokens
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM auth_tokens
		WHERE expires_at < ?
	`, nowStr)
	if err != nil {
		return err
	}

	// Delete expired nonces
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM url_nonces
		WHERE expires_at < ?
	`, nowStr)

	return err
}

// === Sync Lock Methods ===

// AcquireLock attempts to acquire a sync lock for a device.
// Returns ErrSyncLocked if another device already holds the lock.
// Sets 10-min TTL. Uses atomic transaction to prevent TOCTOU race.
func (s *Store) AcquireLock(ctx context.Context, userID int64, equipmentNo string) error {
	now := time.Now()
	expiresAt := now.Add(10 * time.Minute).Format(time.RFC3339Nano)
	nowStr := now.Format(time.RFC3339Nano)

	// Use transaction to make check-and-set atomic
	// SQLite with IMMEDIATE isolation ensures serialization
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if another device already holds an unexpired lock
	query := `
		SELECT equipment_no FROM sync_locks
		WHERE user_id = ? AND expires_at > ?
	`

	var existingDevice string
	err = tx.QueryRowContext(ctx, query, userID, nowStr).Scan(&existingDevice)
	if err == nil && existingDevice != equipmentNo {
		return ErrSyncLocked
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// INSERT OR REPLACE with the new lock
	insertQuery := `
		INSERT OR REPLACE INTO sync_locks (user_id, equipment_no, expires_at)
		VALUES (?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, insertQuery, userID, equipmentNo, expiresAt)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ReleaseLock removes the sync lock for a user.
func (s *Store) ReleaseLock(ctx context.Context, userID int64, equipmentNo string) error {
	query := `
		DELETE FROM sync_locks
		WHERE user_id = ? AND equipment_no = ?
	`

	_, err := s.db.ExecContext(ctx, query, userID, equipmentNo)
	return err
}

// RefreshLock extends the lock expiry to now+10min.
func (s *Store) RefreshLock(ctx context.Context, userID int64) error {
	expiresAt := time.Now().Add(10 * time.Minute).Format(time.RFC3339Nano)

	query := `
		UPDATE sync_locks
		SET expires_at = ?
		WHERE user_id = ?
	`

	_, err := s.db.ExecContext(ctx, query, expiresAt, userID)
	return err
}

// === File Catalog Methods ===

// CreateFile inserts a new file entry into the files table.
// EnsureRootFolders creates the standard SPC root folder structure if missing.
// Root folders: DOCUMENT, NOTE, EXPORT, SCREENSHOT, INBOX
// Sub-folders: DOCUMENT/Document, NOTE/Note, NOTE/MyStyle
func (s *Store) EnsureRootFolders(ctx context.Context, userID int64, genID func() int64) error {
	type folder struct {
		name     string
		parentID int64 // 0 = root
	}

	// First pass: create root folders
	rootFolders := []string{"DOCUMENT", "NOTE", "EXPORT", "SCREENSHOT", "INBOX"}
	rootIDs := map[string]int64{}

	for _, name := range rootFolders {
		var existingID int64
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM files WHERE user_id = ? AND directory_id = 0 AND file_name = ? AND is_folder = 'Y'`,
			userID, name).Scan(&existingID)
		if err == sql.ErrNoRows {
			existingID = genID()
			now := time.Now().Format(time.RFC3339Nano)
			_, err = s.db.ExecContext(ctx,
				`INSERT INTO files (id, user_id, directory_id, file_name, is_folder, is_active, created_at, updated_at) VALUES (?, ?, 0, ?, 'Y', 'Y', ?, ?)`,
				existingID, userID, name, now, now)
			if err != nil {
				return fmt.Errorf("create root folder %s: %w", name, err)
			}
		} else if err != nil {
			return err
		}
		rootIDs[name] = existingID
	}

	// Second pass: create sub-folders
	subFolders := []folder{
		{name: "Document", parentID: rootIDs["DOCUMENT"]},
		{name: "Note", parentID: rootIDs["NOTE"]},
		{name: "MyStyle", parentID: rootIDs["NOTE"]},
	}

	for _, sf := range subFolders {
		var existingID int64
		err := s.db.QueryRowContext(ctx,
			`SELECT id FROM files WHERE user_id = ? AND directory_id = ? AND file_name = ? AND is_folder = 'Y'`,
			userID, sf.parentID, sf.name).Scan(&existingID)
		if err == sql.ErrNoRows {
			now := time.Now().Format(time.RFC3339Nano)
			_, err = s.db.ExecContext(ctx,
				`INSERT INTO files (id, user_id, directory_id, file_name, is_folder, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, 'Y', 'Y', ?, ?)`,
				genID(), userID, sf.parentID, sf.name, now, now)
			if err != nil {
				return fmt.Errorf("create sub-folder %s: %w", sf.name, err)
			}
		} else if err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) CreateFile(ctx context.Context, f *FileEntry) error {
	now := time.Now().Format(time.RFC3339Nano)
	isFolderStr := "N"
	if f.IsFolder {
		isFolderStr = "Y"
	}
	isActiveStr := "Y"
	if !f.IsActive {
		isActiveStr = "N"
	}

	query := `
		INSERT INTO files (id, user_id, directory_id, file_name, inner_name, storage_key, md5, size, is_folder, is_active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		f.ID, f.UserID, f.DirectoryID, f.FileName, f.InnerName, f.StorageKey,
		f.MD5, f.Size, isFolderStr, isActiveStr, now, now)
	return err
}

// GetFile retrieves a file by ID and user_id.
func (s *Store) GetFile(ctx context.Context, id int64, userID int64) (*FileEntry, error) {
	query := `
		SELECT id, user_id, directory_id, file_name, COALESCE(inner_name, ''), COALESCE(storage_key, ''), COALESCE(md5, ''), size, is_folder, is_active, COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM files
		WHERE id = ? AND user_id = ?
	`

	var f FileEntry
	var isFolderStr, isActiveStr string
	var createdAtStr, updatedAtStr string

	err := s.db.QueryRowContext(ctx, query, id, userID).Scan(
		&f.ID, &f.UserID, &f.DirectoryID, &f.FileName, &f.InnerName, &f.StorageKey,
		&f.MD5, &f.Size, &isFolderStr, &isActiveStr, &createdAtStr, &updatedAtStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	f.IsFolder = isFolderStr == "Y"
	f.IsActive = isActiveStr == "Y"
	f.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)

	return &f, nil
}

// GetFileByPath retrieves a file by user_id, directory_id, and file_name.
func (s *Store) GetFileByPath(ctx context.Context, userID int64, directoryID int64, fileName string) (*FileEntry, error) {
	query := `
		SELECT id, user_id, directory_id, file_name, COALESCE(inner_name, ''), COALESCE(storage_key, ''), COALESCE(md5, ''), size, is_folder, is_active, COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM files
		WHERE user_id = ? AND directory_id = ? AND file_name = ? AND is_active = 'Y'
	`

	var f FileEntry
	var isFolderStr, isActiveStr string
	var createdAtStr, updatedAtStr string

	err := s.db.QueryRowContext(ctx, query, userID, directoryID, fileName).Scan(
		&f.ID, &f.UserID, &f.DirectoryID, &f.FileName, &f.InnerName, &f.StorageKey,
		&f.MD5, &f.Size, &isFolderStr, &isActiveStr, &createdAtStr, &updatedAtStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	f.IsFolder = isFolderStr == "Y"
	f.IsActive = isActiveStr == "Y"
	f.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
	f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)

	return &f, nil
}

// UpdateFileMD5 updates the MD5, size, and updated_at timestamp for a file.
func (s *Store) UpdateFileMD5(ctx context.Context, id int64, md5 string, size int64) error {
	now := time.Now().Format(time.RFC3339Nano)

	query := `
		UPDATE files
		SET md5 = ?, size = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, md5, size, now, id)
	return err
}

// ListFolder retrieves all files in a directory, ordered by is_folder DESC then file_name.
func (s *Store) ListFolder(ctx context.Context, userID int64, directoryID int64) ([]FileEntry, error) {
	query := `
		SELECT id, user_id, directory_id, file_name,
			COALESCE(inner_name, ''), COALESCE(storage_key, ''), COALESCE(md5, ''),
			COALESCE(size, 0), is_folder, COALESCE(is_active, 'Y'),
			COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM files
		WHERE user_id = ? AND directory_id = ? AND is_active = 'Y'
		ORDER BY is_folder DESC, file_name
	`

	rows, err := s.db.QueryContext(ctx, query, userID, directoryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []FileEntry
	for rows.Next() {
		var f FileEntry
		var isFolderStr, isActiveStr string
		var createdAtStr, updatedAtStr string

		err := rows.Scan(
			&f.ID, &f.UserID, &f.DirectoryID, &f.FileName, &f.InnerName, &f.StorageKey,
			&f.MD5, &f.Size, &isFolderStr, &isActiveStr, &createdAtStr, &updatedAtStr)
		if err != nil {
			return nil, err
		}

		f.IsFolder = isFolderStr == "Y"
		f.IsActive = isActiveStr == "Y"
		f.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAtStr)
		f.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAtStr)
		entries = append(entries, f)
	}

	return entries, rows.Err()
}

// ListFolderRecursive recursively retrieves all files and folders within a directory using BFS.
func (s *Store) ListFolderRecursive(ctx context.Context, userID int64, directoryID int64) ([]FileEntry, error) {
	var entries []FileEntry
	var queue []int64

	// Start with the root directory
	queue = append(queue, directoryID)

	for len(queue) > 0 {
		currentDir := queue[0]
		queue = queue[1:]

		// List files in current directory
		items, err := s.ListFolder(ctx, userID, currentDir)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			entries = append(entries, item)
			// If it's a folder, add to queue for processing
			if item.IsFolder {
				queue = append(queue, item.ID)
			}
		}
	}

	return entries, nil
}

// SoftDelete moves a file to recycle_files and removes it from files table.
func (s *Store) SoftDelete(ctx context.Context, id int64, userID int64) error {
	// Get the file first
	file, err := s.GetFile(ctx, id, userID)
	if err != nil {
		return err
	}
	if file == nil {
		return errors.New("file not found")
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339Nano)
	isFolderStr := "N"
	if file.IsFolder {
		isFolderStr = "Y"
	}
	isActiveStr := "Y"
	if !file.IsActive {
		isActiveStr = "N"
	}

	// Insert into recycle_files
	insertQuery := `
		INSERT INTO recycle_files (id, user_id, directory_id, file_name, inner_name, storage_key, md5, size, is_folder, is_active, created_at, updated_at, deleted_at, original_directory_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, insertQuery,
		file.ID, file.UserID, file.DirectoryID, file.FileName, file.InnerName, file.StorageKey,
		file.MD5, file.Size, isFolderStr, isActiveStr, file.CreatedAt.Format(time.RFC3339Nano), file.UpdatedAt.Format(time.RFC3339Nano), now, file.DirectoryID)
	if err != nil {
		return err
	}

	// Delete from files
	deleteQuery := `
		DELETE FROM files WHERE id = ? AND user_id = ?
	`

	_, err = tx.ExecContext(ctx, deleteQuery, id, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// MoveFile updates the directory_id and file_name for a file, and updates updated_at.
func (s *Store) MoveFile(ctx context.Context, id int64, newDirectoryID int64, newFileName string) error {
	now := time.Now().Format(time.RFC3339Nano)

	query := `
		UPDATE files
		SET directory_id = ?, file_name = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := s.db.ExecContext(ctx, query, newDirectoryID, newFileName, now, id)
	return err
}

// GetAncestorIDs walks the parent chain starting from directoryID up to limit levels.
// Returns a slice of ancestor IDs (stops at directoryID=0 which is root).
func (s *Store) GetAncestorIDs(ctx context.Context, directoryID int64, limit int) ([]int64, error) {
	var ancestors []int64
	currentID := directoryID
	count := 0

	for currentID != 0 && count < limit {
		ancestors = append(ancestors, currentID)
		count++

		// Get parent directory
		query := `
			SELECT directory_id FROM files WHERE id = ?
		`

		var parentID sql.NullInt64
		err := s.db.QueryRowContext(ctx, query, currentID).Scan(&parentID)
		if err == sql.ErrNoRows {
			break
		}
		if err != nil {
			return nil, err
		}

		if !parentID.Valid {
			break
		}

		currentID = parentID.Int64
	}

	return ancestors, nil
}

// FindByName finds all file names in a directory matching a pattern for autorename detection.
// Returns a slice of matching names.
func (s *Store) FindByName(ctx context.Context, userID int64, directoryID int64, baseName string) ([]string, error) {
	query := `
		SELECT file_name FROM files
		WHERE user_id = ? AND directory_id = ? AND file_name LIKE ?
	`

	// Use % wildcard for LIKE pattern
	pattern := baseName + "%"

	rows, err := s.db.QueryContext(ctx, query, userID, directoryID, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		err := rows.Scan(&name)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}

	return names, rows.Err()
}

// SpaceUsage returns the total size of all non-folder files for a user.
func (s *Store) SpaceUsage(ctx context.Context, userID int64) (int64, error) {
	query := `
		SELECT COALESCE(SUM(size), 0)
		FROM files
		WHERE user_id = ? AND is_folder = 'N'
	`

	var total int64
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&total)
	return total, err
}

// === Chunk Tracking Methods ===

// SaveChunkRecord inserts a chunk upload record into chunk_uploads table.
func (s *Store) SaveChunkRecord(ctx context.Context, uploadID string, partNumber, totalChunks int, md5, path string) error {
	query := `
		INSERT OR REPLACE INTO chunk_uploads (upload_id, part_number, total_chunks, chunk_md5, path)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, uploadID, partNumber, totalChunks, md5, path)
	return err
}

// CountChunks returns the number of chunks recorded for an uploadID.
func (s *Store) CountChunks(ctx context.Context, uploadID string) (int, error) {
	query := `
		SELECT COUNT(*) FROM chunk_uploads WHERE upload_id = ?
	`

	var count int
	err := s.db.QueryRowContext(ctx, query, uploadID).Scan(&count)
	return count, err
}

// DeleteChunkRecords removes all chunk records for an uploadID.
func (s *Store) DeleteChunkRecords(ctx context.Context, uploadID string) error {
	query := `
		DELETE FROM chunk_uploads WHERE upload_id = ?
	`

	_, err := s.db.ExecContext(ctx, query, uploadID)
	return err
}

// DB returns the underlying *sql.DB handle.
func (s *Store) DB() *sql.DB {
	return s.db
}

// GetFileByStorageKey retrieves a file entry by storage key.
// Returns nil if not found (not an error).
func (s *Store) GetFileByStorageKey(ctx context.Context, userID int64, storageKey string) (*FileEntry, error) {
	query := `
		SELECT id, user_id, directory_id, file_name, COALESCE(inner_name, ''), COALESCE(storage_key, ''), COALESCE(md5, ''), size, is_folder, is_active, COALESCE(created_at, ''), COALESCE(updated_at, '')
		FROM files
		WHERE user_id = ? AND storage_key = ? AND is_active = 'Y'
	`

	var entry FileEntry
	var isFolder, isActive string
	var createdAtStr, updatedAtStr string

	err := s.db.QueryRowContext(ctx, query, userID, storageKey).Scan(
		&entry.ID,
		&entry.UserID,
		&entry.DirectoryID,
		&entry.FileName,
		&entry.InnerName,
		&entry.StorageKey,
		&entry.MD5,
		&entry.Size,
		&isFolder,
		&isActive,
		&createdAtStr,
		&updatedAtStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found, not an error
	}
	if err != nil {
		return nil, err
	}

	entry.IsFolder = isFolder == "Y"
	entry.IsActive = isActive == "Y"
	if t, err := time.Parse(time.RFC3339Nano, createdAtStr); err != nil {
		slog.Warn("GetFileByStorageKey: failed to parse created_at", "value", createdAtStr, "err", err)
	} else {
		entry.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updatedAtStr); err != nil {
		slog.Warn("GetFileByStorageKey: failed to parse updated_at", "value", updatedAtStr, "err", err)
	} else {
		entry.UpdatedAt = t
	}

	return &entry, nil
}

// === Schedule Group Methods ===

// generateTaskListID generates a task list ID from title and timestamp using MD5.
// If collision is detected, increments a suffix until unique.
func (s *Store) generateTaskListID(ctx context.Context, userID int64, title string, lastModified int64) (string, error) {
	// Generate base ID: MD5(title + lastModified)
	input := fmt.Sprintf("%s%d", title, lastModified)
	hash := md5.Sum([]byte(input))
	baseID := hex.EncodeToString(hash[:])

	// Check if ID exists
	query := `SELECT 1 FROM schedule_groups WHERE task_list_id = ? AND user_id = ?`
	var dummy int
	err := s.db.QueryRowContext(ctx, query, baseID, userID).Scan(&dummy)

	if err == sql.ErrNoRows {
		return baseID, nil // No collision
	}
	if err != nil {
		return "", err
	}

	// Collision detected; try with suffix
	for i := 1; i <= 1000; i++ {
		suffixID := baseID + strconv.Itoa(i)
		err := s.db.QueryRowContext(ctx, query, suffixID, userID).Scan(&dummy)
		if err == sql.ErrNoRows {
			return suffixID, nil
		}
		if err != nil {
			return "", err
		}
	}

	return "", errors.New("unable to generate unique task list ID")
}

// UpsertScheduleGroup inserts or replaces a schedule group.
// If taskListID is empty, generates a new one via MD5(title+lastModified) with collision incrementing.
func (s *Store) UpsertScheduleGroup(ctx context.Context, g *ScheduleGroup) error {
	// Generate ID if empty
	if g.TaskListID == "" {
		id, err := s.generateTaskListID(ctx, g.UserID, g.Title, g.LastModified)
		if err != nil {
			return err
		}
		g.TaskListID = id
	}

	now := time.Now().Format(time.RFC3339Nano)

	query := `
		INSERT OR REPLACE INTO schedule_groups (task_list_id, user_id, title, last_modified, create_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query, g.TaskListID, g.UserID, g.Title, g.LastModified, g.CreateTime, now)
	return err
}

// UpdateScheduleGroup partially updates a schedule group.
func (s *Store) UpdateScheduleGroup(ctx context.Context, taskListID string, userID int64, updates map[string]interface{}) error {
	// Validate field names against allowlist
	validatedUpdates, err := validateAndFilterFields(updates, scheduleGroupAllowedFields)
	if err != nil {
		return err
	}

	// Check if group exists
	query := `SELECT 1 FROM schedule_groups WHERE task_list_id = ? AND user_id = ?`
	var dummy int
	err = s.db.QueryRowContext(ctx, query, taskListID, userID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return ErrTaskGroupNotFound
	}
	if err != nil {
		return err
	}

	// Build dynamic update query
	updateStr := "updated_at = ?"
	args := []interface{}{time.Now().Format(time.RFC3339Nano)}

	for key, value := range validatedUpdates {
		updateStr += fmt.Sprintf(", %s = ?", key)
		args = append(args, value)
	}

	args = append(args, taskListID, userID)

	updateQuery := fmt.Sprintf(`
		UPDATE schedule_groups
		SET %s
		WHERE task_list_id = ? AND user_id = ?
	`, updateStr)

	_, err = s.db.ExecContext(ctx, updateQuery, args...)
	return err
}

// DeleteScheduleGroup deletes a group and all its tasks (cascading).
func (s *Store) DeleteScheduleGroup(ctx context.Context, taskListID string, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all tasks in group
	deleteTasksQuery := `DELETE FROM schedule_tasks WHERE task_list_id = ? AND user_id = ?`
	_, err = tx.ExecContext(ctx, deleteTasksQuery, taskListID, userID)
	if err != nil {
		return err
	}

	// Delete group
	deleteGroupQuery := `DELETE FROM schedule_groups WHERE task_list_id = ? AND user_id = ?`
	_, err = tx.ExecContext(ctx, deleteGroupQuery, taskListID, userID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// ListScheduleGroups returns a paginated list of schedule groups for a user.
func (s *Store) ListScheduleGroups(ctx context.Context, userID int64, page, pageSize int) ([]ScheduleGroup, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM schedule_groups WHERE user_id = ?`
	var totalCount int
	err := s.db.QueryRowContext(ctx, countQuery, userID).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	query := `
		SELECT task_list_id, user_id, title, last_modified, create_time
		FROM schedule_groups
		WHERE user_id = ?
		ORDER BY task_list_id
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, userID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var groups []ScheduleGroup
	for rows.Next() {
		var g ScheduleGroup
		err := rows.Scan(&g.TaskListID, &g.UserID, &g.Title, &g.LastModified, &g.CreateTime)
		if err != nil {
			return nil, 0, err
		}
		groups = append(groups, g)
	}

	return groups, totalCount, rows.Err()
}

// === Schedule Task Methods ===

// generateTaskID generates a random nonce for a task ID.
func (s *Store) generateTaskID() (string, error) {
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes), nil
}

// UpsertScheduleTask inserts or replaces a schedule task.
// If taskID is empty, generates a random nonce.
// Validates that taskListID exists if provided.
func (s *Store) UpsertScheduleTask(ctx context.Context, t *ScheduleTask) error {
	// Generate ID if empty
	if t.TaskID == "" {
		id, err := s.generateTaskID()
		if err != nil {
			return err
		}
		t.TaskID = id
	}

	// Validate taskListID exists
	if t.TaskListID != "" {
		query := `SELECT 1 FROM schedule_groups WHERE task_list_id = ? AND user_id = ?`
		var dummy int
		err := s.db.QueryRowContext(ctx, query, t.TaskListID, t.UserID).Scan(&dummy)
		if err == sql.ErrNoRows {
			return ErrTaskGroupNotFound
		}
		if err != nil {
			return err
		}
	}

	now := time.Now().Format(time.RFC3339Nano)

	query := `
		INSERT OR REPLACE INTO schedule_tasks
		(task_id, user_id, task_list_id, title, detail, status, importance, recurrence, links, is_reminder_on,
		 due_time, completed_time, last_modified, sort, sort_completed, planer_sort, sort_time, planer_sort_time,
		 all_sort, all_sort_completed, all_sort_time, recurrence_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		t.TaskID, t.UserID, t.TaskListID, t.Title, t.Detail, t.Status, t.Importance, t.Recurrence, t.Links, t.IsReminderOn,
		t.DueTime, t.CompletedTime, t.LastModified, t.Sort, t.SortCompleted, t.PlanerSort, t.SortTime, t.PlanerSortTime,
		t.AllSort, t.AllSortCompleted, t.AllSortTime, t.RecurrenceID, now)
	return err
}

// BatchUpdateTasks atomically updates multiple tasks.
// Validates all taskIDs exist first, then applies updates in a transaction.
func (s *Store) BatchUpdateTasks(ctx context.Context, userID int64, tasks []TaskUpdate) error {
	if len(tasks) == 0 {
		return nil
	}

	// Validate all taskIDs exist
	for _, tu := range tasks {
		query := `SELECT 1 FROM schedule_tasks WHERE task_id = ? AND user_id = ?`
		var dummy int
		err := s.db.QueryRowContext(ctx, query, tu.TaskID, userID).Scan(&dummy)
		if err == sql.ErrNoRows {
			return ErrTaskNotFound
		}
		if err != nil {
			return err
		}
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Format(time.RFC3339Nano)

	// Apply updates
	for _, tu := range tasks {
		// Validate field names against allowlist
		validatedUpdates, err := validateAndFilterFields(tu.Fields, scheduleTaskAllowedFields)
		if err != nil {
			return err
		}

		updateStr := "updated_at = ?"
		args := []interface{}{now}

		for key, value := range validatedUpdates {
			updateStr += fmt.Sprintf(", %s = ?", key)
			args = append(args, value)
		}

		args = append(args, tu.TaskID, userID)

		updateQuery := fmt.Sprintf(`
			UPDATE schedule_tasks
			SET %s
			WHERE task_id = ? AND user_id = ?
		`, updateStr)

		_, err = tx.ExecContext(ctx, updateQuery, args...)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteScheduleTask deletes a task.
func (s *Store) DeleteScheduleTask(ctx context.Context, taskID string, userID int64) error {
	query := `DELETE FROM schedule_tasks WHERE task_id = ? AND user_id = ?`
	_, err := s.db.ExecContext(ctx, query, taskID, userID)
	return err
}

// ListScheduleTasks returns a paginated list of tasks with optional sync token filtering.
// Sorted by last_modified DESC. If syncToken provided, filters to tasks where updated_at >= syncToken.
// Returns tasks, totalCount, nextSyncToken (current time as millis only on final page), error
func (s *Store) ListScheduleTasks(ctx context.Context, userID int64, page, pageSize int, syncToken *int64) ([]ScheduleTask, int, *int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Get total count with optional sync token filter
	var countQuery string
	var countArgs []interface{}

	if syncToken != nil {
		syncTime := time.UnixMilli(*syncToken).Format(time.RFC3339Nano)
		countQuery = `SELECT COUNT(*) FROM schedule_tasks WHERE user_id = ? AND updated_at >= ?`
		countArgs = append(countArgs, userID, syncTime)
	} else {
		countQuery = `SELECT COUNT(*) FROM schedule_tasks WHERE user_id = ?`
		countArgs = append(countArgs, userID)
	}

	var totalCount int
	err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, 0, nil, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	isLastPage := (offset + pageSize) >= totalCount

	var query string
	var args []interface{}

	if syncToken != nil {
		syncTime := time.UnixMilli(*syncToken).Format(time.RFC3339Nano)
		query = `
			SELECT task_id, user_id, task_list_id, title, detail, status, importance, recurrence, links, is_reminder_on,
				   due_time, completed_time, last_modified, sort, sort_completed, planer_sort, sort_time, planer_sort_time,
				   all_sort, all_sort_completed, all_sort_time, recurrence_id
			FROM schedule_tasks
			WHERE user_id = ? AND updated_at >= ?
			ORDER BY last_modified DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, syncTime, pageSize, offset}
	} else {
		query = `
			SELECT task_id, user_id, task_list_id, title, detail, status, importance, recurrence, links, is_reminder_on,
				   due_time, completed_time, last_modified, sort, sort_completed, planer_sort, sort_time, planer_sort_time,
				   all_sort, all_sort_completed, all_sort_time, recurrence_id
			FROM schedule_tasks
			WHERE user_id = ?
			ORDER BY last_modified DESC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, pageSize, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, nil, err
	}
	defer rows.Close()

	var tasks []ScheduleTask
	for rows.Next() {
		var t ScheduleTask

		err := rows.Scan(&t.TaskID, &t.UserID, &t.TaskListID, &t.Title, &t.Detail, &t.Status, &t.Importance, &t.Recurrence, &t.Links, &t.IsReminderOn,
			&t.DueTime, &t.CompletedTime, &t.LastModified, &t.Sort, &t.SortCompleted, &t.PlanerSort, &t.SortTime, &t.PlanerSortTime,
			&t.AllSort, &t.AllSortCompleted, &t.AllSortTime, &t.RecurrenceID)
		if err != nil {
			return nil, 0, nil, err
		}

		tasks = append(tasks, t)
	}

	// Return nextSyncToken only on final page
	var nextToken *int64
	if isLastPage {
		now := time.Now().UnixMilli()
		nextToken = &now
	}

	return tasks, totalCount, nextToken, rows.Err()
}

// === Summary Methods ===

// CreateSummary inserts a new summary with Snowflake ID.
// Checks uniqueness on (user_id, unique_identifier).
func (s *Store) CreateSummary(ctx context.Context, sum *Summary) error {
	// Check uniqueness
	query := `SELECT 1 FROM summaries WHERE user_id = ? AND unique_identifier = ?`
	var dummy int
	err := s.db.QueryRowContext(ctx, query, sum.UserID, sum.UniqueIdentifier).Scan(&dummy)
	if err == nil {
		return ErrUniqueIDExists // Already exists
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	now := time.Now().Format(time.RFC3339Nano)

	insertQuery := `
		INSERT INTO summaries
		(id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
		 data_source, source_path, source_type, tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
		 comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, insertQuery,
		sum.ID, sum.UserID, sum.UniqueIdentifier, sum.Name, sum.Description, sum.FileID, sum.ParentUniqueIdentifier, sum.Content,
		sum.DataSource, sum.SourcePath, sum.SourceType, sum.Tags, sum.MD5Hash, sum.HandwriteMD5, sum.HandwriteInnerName, sum.Metadata,
		sum.CommentStr, &sum.HandwriteFields, sum.CommentHandwriteName, sum.IsSummaryGroup, sum.Author, sum.CreationTime, sum.LastModifiedTime, now)
	return err
}

// UpdateSummary partially updates a summary.
func (s *Store) UpdateSummary(ctx context.Context, id int64, userID int64, updates map[string]interface{}) error {
	// Validate field names against allowlist
	validatedUpdates, err := validateAndFilterFields(updates, summaryAllowedFields)
	if err != nil {
		return err
	}

	// Check if summary exists
	query := `SELECT 1 FROM summaries WHERE id = ? AND user_id = ?`
	var dummy int
	err = s.db.QueryRowContext(ctx, query, id, userID).Scan(&dummy)
	if err == sql.ErrNoRows {
		return ErrSummaryNotFound
	}
	if err != nil {
		return err
	}

	// Build dynamic update query
	updateStr := "updated_at = ?"
	args := []interface{}{time.Now().Format(time.RFC3339Nano)}

	for key, value := range validatedUpdates {
		updateStr += fmt.Sprintf(", %s = ?", key)
		args = append(args, value)
	}

	args = append(args, id, userID)

	updateQuery := fmt.Sprintf(`
		UPDATE summaries
		SET %s
		WHERE id = ? AND user_id = ?
	`, updateStr)

	_, err = s.db.ExecContext(ctx, updateQuery, args...)
	return err
}

// DeleteSummary deletes a summary.
func (s *Store) DeleteSummary(ctx context.Context, id int64, userID int64) error {
	query := `DELETE FROM summaries WHERE id = ? AND user_id = ?`
	_, err := s.db.ExecContext(ctx, query, id, userID)
	return err
}

// ListSummaryGroups returns summary groups (is_summary_group='Y') paginated.
func (s *Store) ListSummaryGroups(ctx context.Context, userID int64, page, pageSize int) ([]Summary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM summaries WHERE user_id = ? AND is_summary_group = 'Y'`
	var totalCount int
	err := s.db.QueryRowContext(ctx, countQuery, userID).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	query := `
		SELECT id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
		       data_source, source_path, CAST(COALESCE(NULLIF(source_type, ''), '0') AS INTEGER), tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
		       comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time
		FROM summaries
		WHERE user_id = ? AND is_summary_group = 'Y'
		ORDER BY id
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, userID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var summaries []Summary
	for rows.Next() {
		var sum Summary
		var creationTimeStr, lastModifiedTimeStr sql.NullString

		err := rows.Scan(&sum.ID, &sum.UserID, &sum.UniqueIdentifier, &sum.Name, &sum.Description, &sum.FileID, &sum.ParentUniqueIdentifier, &sum.Content,
			&sum.DataSource, &sum.SourcePath, &sum.SourceType, &sum.Tags, &sum.MD5Hash, &sum.HandwriteMD5, &sum.HandwriteInnerName, &sum.Metadata,
			&sum.CommentStr, &sum.HandwriteFields, &sum.CommentHandwriteName, &sum.IsSummaryGroup, &sum.Author, &creationTimeStr, &lastModifiedTimeStr)
		if err != nil {
			return nil, 0, err
		}

		// Parse datetime fields
		if creationTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, creationTimeStr.String); err == nil {
				sum.CreationTime = tm.Unix() * 1000
			}
		}
		if lastModifiedTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, lastModifiedTimeStr.String); err == nil {
				sum.LastModifiedTime = tm.Unix() * 1000
			}
		}

		summaries = append(summaries, sum)
	}

	return summaries, totalCount, rows.Err()
}

// ListSummaries returns summaries (is_summary_group='N') paginated, optionally filtered by parentUID.
func (s *Store) ListSummaries(ctx context.Context, userID int64, page, pageSize int, parentUID *string) ([]Summary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Get total count
	var countQuery string
	var countArgs []interface{}

	if parentUID != nil {
		countQuery = `SELECT COUNT(*) FROM summaries WHERE user_id = ? AND is_summary_group = 'N' AND parent_unique_identifier = ?`
		countArgs = append(countArgs, userID, *parentUID)
	} else {
		countQuery = `SELECT COUNT(*) FROM summaries WHERE user_id = ? AND is_summary_group = 'N'`
		countArgs = append(countArgs, userID)
	}

	var totalCount int
	err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	var query string
	var args []interface{}

	if parentUID != nil {
		query = `
			SELECT id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
			       data_source, source_path, CAST(COALESCE(NULLIF(source_type, ''), '0') AS INTEGER), tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
			       comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time
			FROM summaries
			WHERE user_id = ? AND is_summary_group = 'N' AND parent_unique_identifier = ?
			ORDER BY id
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, *parentUID, pageSize, offset}
	} else {
		query = `
			SELECT id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
			       data_source, source_path, CAST(COALESCE(NULLIF(source_type, ''), '0') AS INTEGER), tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
			       comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time
			FROM summaries
			WHERE user_id = ? AND is_summary_group = 'N'
			ORDER BY id
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, pageSize, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var summaries []Summary
	for rows.Next() {
		var sum Summary
		var creationTimeStr, lastModifiedTimeStr sql.NullString

		err := rows.Scan(&sum.ID, &sum.UserID, &sum.UniqueIdentifier, &sum.Name, &sum.Description, &sum.FileID, &sum.ParentUniqueIdentifier, &sum.Content,
			&sum.DataSource, &sum.SourcePath, &sum.SourceType, &sum.Tags, &sum.MD5Hash, &sum.HandwriteMD5, &sum.HandwriteInnerName, &sum.Metadata,
			&sum.CommentStr, &sum.HandwriteFields, &sum.CommentHandwriteName, &sum.IsSummaryGroup, &sum.Author, &creationTimeStr, &lastModifiedTimeStr)
		if err != nil {
			return nil, 0, err
		}

		// Parse datetime fields
		if creationTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, creationTimeStr.String); err == nil {
				sum.CreationTime = tm.Unix() * 1000
			}
		}
		if lastModifiedTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, lastModifiedTimeStr.String); err == nil {
				sum.LastModifiedTime = tm.Unix() * 1000
			}
		}

		summaries = append(summaries, sum)
	}

	return summaries, totalCount, rows.Err()
}

// ListSummaryHashes returns lightweight hash data for summaries, optionally filtered by parentUID.
func (s *Store) ListSummaryHashes(ctx context.Context, userID int64, page, pageSize int, parentUID *string) ([]SummaryHash, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Get total count
	var countQuery string
	var countArgs []interface{}

	if parentUID != nil {
		countQuery = `SELECT COUNT(*) FROM summaries WHERE user_id = ? AND parent_unique_identifier = ?`
		countArgs = append(countArgs, userID, *parentUID)
	} else {
		countQuery = `SELECT COUNT(*) FROM summaries WHERE user_id = ?`
		countArgs = append(countArgs, userID)
	}

	var totalCount int
	err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	var query string
	var args []interface{}

	if parentUID != nil {
		query = `
			SELECT id, md5_hash, handwrite_md5, comment_handwrite_name, last_modified_time, metadata
			FROM summaries
			WHERE user_id = ? AND parent_unique_identifier = ?
			ORDER BY id
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, *parentUID, pageSize, offset}
	} else {
		query = `
			SELECT id, md5_hash, handwrite_md5, comment_handwrite_name, last_modified_time, metadata
			FROM summaries
			WHERE user_id = ?
			ORDER BY id
			LIMIT ? OFFSET ?
		`
		args = []interface{}{userID, pageSize, offset}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var hashes []SummaryHash
	for rows.Next() {
		var h SummaryHash
		var lastModifiedTimeStr sql.NullString

		err := rows.Scan(&h.ID, &h.MD5Hash, &h.HandwriteMD5, &h.CommentHandwriteName, &lastModifiedTimeStr, &h.Metadata)
		if err != nil {
			return nil, 0, err
		}

		// Parse datetime field
		if lastModifiedTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, lastModifiedTimeStr.String); err == nil {
				h.LastModifiedTime = tm.Unix() * 1000
			}
		}

		hashes = append(hashes, h)
	}

	return hashes, totalCount, rows.Err()
}

// GetSummariesByIDs returns summaries matching specific IDs, paginated.
func (s *Store) GetSummariesByIDs(ctx context.Context, userID int64, ids []int64, page, pageSize int) ([]Summary, int, error) {
	if len(ids) == 0 {
		return []Summary{}, 0, nil
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	// Build IN clause
	placeholders := ""
	args := []interface{}{userID}
	for _, id := range ids {
		if placeholders != "" {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}

	// Get total count
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM summaries WHERE user_id = ? AND id IN (%s)`, placeholders)
	var totalCount int
	countArgs := args
	err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, 0, err
	}

	// Get paginated results
	offset := (page - 1) * pageSize
	query := fmt.Sprintf(`
		SELECT id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
		       data_source, source_path, CAST(COALESCE(NULLIF(source_type, ''), '0') AS INTEGER), tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
		       comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time
		FROM summaries
		WHERE user_id = ? AND id IN (%s)
		ORDER BY id
		LIMIT ? OFFSET ?
	`, placeholders)

	queryArgs := args
	queryArgs = append(queryArgs, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var summaries []Summary
	for rows.Next() {
		var sum Summary
		var creationTimeStr, lastModifiedTimeStr sql.NullString

		err := rows.Scan(&sum.ID, &sum.UserID, &sum.UniqueIdentifier, &sum.Name, &sum.Description, &sum.FileID, &sum.ParentUniqueIdentifier, &sum.Content,
			&sum.DataSource, &sum.SourcePath, &sum.SourceType, &sum.Tags, &sum.MD5Hash, &sum.HandwriteMD5, &sum.HandwriteInnerName, &sum.Metadata,
			&sum.CommentStr, &sum.HandwriteFields, &sum.CommentHandwriteName, &sum.IsSummaryGroup, &sum.Author, &creationTimeStr, &lastModifiedTimeStr)
		if err != nil {
			return nil, 0, err
		}

		// Parse datetime fields
		if creationTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, creationTimeStr.String); err == nil {
				sum.CreationTime = tm.Unix() * 1000
			}
		}
		if lastModifiedTimeStr.Valid {
			if tm, err := time.Parse(time.RFC3339Nano, lastModifiedTimeStr.String); err == nil {
				sum.LastModifiedTime = tm.Unix() * 1000
			}
		}

		summaries = append(summaries, sum)
	}

	return summaries, totalCount, rows.Err()
}

// GetSummary retrieves a single summary by ID.
func (s *Store) GetSummary(ctx context.Context, id int64, userID int64) (*Summary, error) {
	query := `
		SELECT id, user_id, unique_identifier, name, description, file_id, parent_unique_identifier, content,
		       data_source, source_path, CAST(COALESCE(NULLIF(source_type, ''), '0') AS INTEGER), tags, md5_hash, handwrite_md5, handwrite_inner_name, metadata,
		       comment_fields, handwrite_fields, comment_handwrite_name, is_summary_group, author, creation_time, last_modified_time
		FROM summaries
		WHERE id = ? AND user_id = ?
	`

	var sum Summary
	var creationTimeStr, lastModifiedTimeStr sql.NullString

	err := s.db.QueryRowContext(ctx, query, id, userID).Scan(
		&sum.ID, &sum.UserID, &sum.UniqueIdentifier, &sum.Name, &sum.Description, &sum.FileID, &sum.ParentUniqueIdentifier, &sum.Content,
		&sum.DataSource, &sum.SourcePath, &sum.SourceType, &sum.Tags, &sum.MD5Hash, &sum.HandwriteMD5, &sum.HandwriteInnerName, &sum.Metadata,
		&sum.CommentStr, &sum.HandwriteFields, &sum.CommentHandwriteName, &sum.IsSummaryGroup, &sum.Author, &creationTimeStr, &lastModifiedTimeStr)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse datetime fields
	if creationTimeStr.Valid {
		if tm, err := time.Parse(time.RFC3339Nano, creationTimeStr.String); err == nil {
			sum.CreationTime = tm.Unix() * 1000
		}
	}
	if lastModifiedTimeStr.Valid {
		if tm, err := time.Parse(time.RFC3339Nano, lastModifiedTimeStr.String); err == nil {
			sum.LastModifiedTime = tm.Unix() * 1000
		}
	}

	return &sum, nil
}
