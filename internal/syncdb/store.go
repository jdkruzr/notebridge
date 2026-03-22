package syncdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var ErrSyncLocked = errors.New("sync locked by another device")

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

// NewStore creates a new Store instance.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// EnsureUser inserts a user if it doesn't exist (INSERT OR IGNORE).
// If snowflake is provided, it's used to generate the user ID; otherwise SQLite auto-increments.
func (s *Store) EnsureUser(ctx context.Context, email, passwordHash string, snowflake interface{}) error {
	query := `
		INSERT OR IGNORE INTO users (email, password_hash, error_count, last_error_at, locked_until)
		VALUES (?, ?, 0, NULL, NULL)
	`

	_, err := s.db.ExecContext(ctx, query, email, passwordHash)
	return err
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
// Sets 10-min TTL.
func (s *Store) AcquireLock(ctx context.Context, userID int64, equipmentNo string) error {
	now := time.Now()
	expiresAt := now.Add(10 * time.Minute).Format(time.RFC3339Nano)
	nowStr := now.Format(time.RFC3339Nano)

	// Check if another device already holds an unexpired lock
	query := `
		SELECT equipment_no FROM sync_locks
		WHERE user_id = ? AND expires_at > ?
	`

	var existingDevice string
	err := s.db.QueryRowContext(ctx, query, userID, nowStr).Scan(&existingDevice)
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

	_, err = s.db.ExecContext(ctx, insertQuery, userID, equipmentNo, expiresAt)
	return err
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
		SELECT id, user_id, directory_id, file_name, inner_name, storage_key, md5, size, is_folder, is_active, created_at, updated_at
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
		SELECT id, user_id, directory_id, file_name, inner_name, storage_key, md5, size, is_folder, is_active, created_at, updated_at
		FROM files
		WHERE user_id = ? AND directory_id = ? AND file_name = ?
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
		SELECT id, user_id, directory_id, file_name, inner_name, storage_key, md5, size, is_folder, is_active, created_at, updated_at
		FROM files
		WHERE user_id = ? AND directory_id = ?
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
