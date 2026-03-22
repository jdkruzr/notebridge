package syncdb

import (
	"context"
	"time"
)

// ExpireNonce manually expires a nonce for testing purposes.
// This is used to test that expired signed URLs are rejected.
func ExpireNonce(ctx context.Context, store *Store, nonce string) error {
	query := `
		UPDATE url_nonces
		SET expires_at = ?
		WHERE nonce = ?
	`

	// Set expiry to 1 hour in the past
	expiryTime := time.Now().Add(-time.Hour)
	_, err := store.db.ExecContext(ctx, query, expiryTime, nonce)
	return err
}

// ExpireLock manually expires a user's sync lock for testing purposes.
// This is used to test that expired locks can be overwritten.
func ExpireLock(ctx context.Context, store *Store, userID int64) error {
	query := `
		UPDATE sync_locks
		SET expires_at = ?
		WHERE user_id = ?
	`

	// Set expiry to 1 hour in the past
	expiryTime := time.Now().Add(-time.Hour)
	_, err := store.db.ExecContext(ctx, query, expiryTime, userID)
	return err
}
