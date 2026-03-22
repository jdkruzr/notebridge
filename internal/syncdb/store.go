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
