package syncdb

import (
	"context"
	"testing"
	"time"
)

func TestEnsureUser(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// First call should create user
	err = store.EnsureUser(ctx, "test@example.com", "passwordhash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	// Second call should be no-op
	err = store.EnsureUser(ctx, "test@example.com", "differenthash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user second time: %v", err)
	}

	// Verify user was created
	user, err := store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if user == nil {
		t.Fatal("user not found")
	}
	if user.Email != "test@example.com" {
		t.Errorf("expected email test@example.com, got %s", user.Email)
	}
	if user.PasswordHash != "passwordhash" {
		t.Errorf("expected passwordhash, got %s", user.PasswordHash)
	}
}

func TestGetUserByEmail(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Nonexistent user should return nil, not error
	user, err := store.GetUserByEmail(ctx, "nonexistent@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if user != nil {
		t.Fatal("expected nil for nonexistent user")
	}

	// Create and retrieve user
	err = store.EnsureUser(ctx, "test@example.com", "hash123", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, err = store.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if user == nil {
		t.Fatal("user not found")
	}
	if user.Email != "test@example.com" {
		t.Errorf("expected test@example.com, got %s", user.Email)
	}
}

func TestErrorCount(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Increment error count
	err = store.IncrementErrorCount(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to increment error count: %v", err)
	}

	user, _ = store.GetUserByEmail(ctx, "test@example.com")
	if user.ErrorCount != 1 {
		t.Errorf("expected error_count=1, got %d", user.ErrorCount)
	}

	// Increment again
	err = store.IncrementErrorCount(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to increment error count: %v", err)
	}

	user, _ = store.GetUserByEmail(ctx, "test@example.com")
	if user.ErrorCount != 2 {
		t.Errorf("expected error_count=2, got %d", user.ErrorCount)
	}

	// Reset error count
	err = store.ResetErrorCount(ctx, user.ID)
	if err != nil {
		t.Fatalf("failed to reset error count: %v", err)
	}

	user, _ = store.GetUserByEmail(ctx, "test@example.com")
	if user.ErrorCount != 0 {
		t.Errorf("expected error_count=0, got %d", user.ErrorCount)
	}
}

func TestLockUser(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Lock user until 1 hour from now
	lockUntil := time.Now().Add(time.Hour)
	err = store.LockUser(ctx, user.ID, lockUntil)
	if err != nil {
		t.Fatalf("failed to lock user: %v", err)
	}

	user, _ = store.GetUserByEmail(ctx, "test@example.com")
	if user.LockedUntil == nil {
		t.Fatal("expected LockedUntil to be set")
	}
	if user.LockedUntil.Before(lockUntil) || user.LockedUntil.After(lockUntil.Add(time.Second)) {
		t.Errorf("locked_until time mismatch: expected ~%v, got %v", lockUntil, user.LockedUntil)
	}
}

func TestChallenge(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create challenge
	now := time.Now().Unix()
	err = store.CreateChallenge(ctx, "test@example.com", "ABC123", now)
	if err != nil {
		t.Fatalf("failed to create challenge: %v", err)
	}

	// Retrieve challenge
	code, err := store.GetChallenge(ctx, "test@example.com", now)
	if err != nil {
		t.Fatalf("failed to get challenge: %v", err)
	}
	if code != "ABC123" {
		t.Errorf("expected ABC123, got %s", code)
	}

	// Delete challenge
	err = store.DeleteChallenge(ctx, "test@example.com", now)
	if err != nil {
		t.Fatalf("failed to delete challenge: %v", err)
	}

	// Should not be retrievable after delete
	code, err = store.GetChallenge(ctx, "test@example.com", now)
	if err == nil {
		t.Fatalf("expected error for deleted challenge")
	}
}

func TestToken(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Store token
	expiresAt := time.Now().Add(time.Hour)
	err = store.StoreToken(ctx, "key123", "token_value", user.ID, "device1", expiresAt)
	if err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Retrieve token
	token, err := store.GetToken(ctx, "key123")
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}
	if token == nil {
		t.Fatal("token not found")
	}
	if token.Token != "token_value" {
		t.Errorf("expected token_value, got %s", token.Token)
	}
	if token.UserID != user.ID {
		t.Errorf("expected userID %d, got %d", user.ID, token.UserID)
	}

	// Delete token
	err = store.DeleteToken(ctx, "key123")
	if err != nil {
		t.Fatalf("failed to delete token: %v", err)
	}

	// Should not be retrievable after delete
	token, err = store.GetToken(ctx, "key123")
	if err == nil {
		t.Fatalf("expected error for deleted token")
	}
}

func TestGetTokenExpired(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Store expired token
	expiresAt := time.Now().Add(-time.Hour) // Already expired
	err = store.StoreToken(ctx, "key_expired", "token_value", user.ID, "device1", expiresAt)
	if err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Retrieve should return nil for expired token
	_, err = store.GetToken(ctx, "key_expired")
	if err == nil {
		t.Fatalf("expected error for expired token")
	}
}

func TestGetOrCreateJWTSecret(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// First call should create
	secret1, err := store.GetOrCreateJWTSecret(ctx)
	if err != nil {
		t.Fatalf("failed to get/create jwt secret: %v", err)
	}
	if secret1 == "" {
		t.Fatal("jwt secret is empty")
	}

	// Second call should return same
	secret2, err := store.GetOrCreateJWTSecret(ctx)
	if err != nil {
		t.Fatalf("failed to get/create jwt secret: %v", err)
	}
	if secret1 != secret2 {
		t.Errorf("secrets differ: %s != %s", secret1, secret2)
	}
}

func TestEnsureEquipment(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Ensure equipment
	err = store.EnsureEquipment(ctx, "device123", user.ID)
	if err != nil {
		t.Fatalf("failed to ensure equipment: %v", err)
	}

	// Second call should be no-op
	err = store.EnsureEquipment(ctx, "device123", user.ID)
	if err != nil {
		t.Fatalf("failed to ensure equipment second time: %v", err)
	}
}

func TestNonce(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Store nonce
	nonce := "nonce123"
	expiresAt := time.Now().Add(time.Hour)
	err = store.StoreNonce(ctx, nonce, expiresAt)
	if err != nil {
		t.Fatalf("failed to store nonce: %v", err)
	}

	// First consume should return true
	consumed, err := store.ConsumeNonce(ctx, nonce)
	if err != nil {
		t.Fatalf("failed to consume nonce: %v", err)
	}
	if !consumed {
		t.Fatal("expected nonce to be consumed")
	}

	// Second consume should return false (single-use)
	consumed, err = store.ConsumeNonce(ctx, nonce)
	if err != nil {
		t.Fatalf("failed to consume nonce second time: %v", err)
	}
	if consumed {
		t.Fatal("expected nonce to not be consumed second time")
	}
}

func TestConsumeExpiredNonce(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Store expired nonce
	nonce := "nonce_expired"
	expiresAt := time.Now().Add(-time.Hour) // Already expired
	err = store.StoreNonce(ctx, nonce, expiresAt)
	if err != nil {
		t.Fatalf("failed to store nonce: %v", err)
	}

	// Consume should return false for expired nonce
	consumed, err := store.ConsumeNonce(ctx, nonce)
	if err != nil {
		t.Fatalf("failed to consume nonce: %v", err)
	}
	if consumed {
		t.Fatal("expected nonce to not be consumed (expired)")
	}
}

func TestCleanupExpired(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := NewStore(db)
	ctx := context.Background()

	// Create user
	err = store.EnsureUser(ctx, "test@example.com", "hash", nil)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Create expired challenge
	oldTime := time.Now().Add(-24 * time.Hour).Unix()
	err = store.CreateChallenge(ctx, "test@example.com", "OLD_CODE", oldTime)
	if err != nil {
		t.Fatalf("failed to create challenge: %v", err)
	}

	// Create expired token
	expiredTime := time.Now().Add(-time.Hour)
	err = store.StoreToken(ctx, "old_key", "old_token", user.ID, "device1", expiredTime)
	if err != nil {
		t.Fatalf("failed to store token: %v", err)
	}

	// Create expired nonce
	err = store.StoreNonce(ctx, "old_nonce", expiredTime)
	if err != nil {
		t.Fatalf("failed to store nonce: %v", err)
	}

	// Run cleanup
	err = store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("failed to cleanup expired: %v", err)
	}

	// Verify expired items are deleted
	code, err := store.GetChallenge(ctx, "test@example.com", oldTime)
	if err == nil {
		t.Errorf("expected error for deleted challenge, got code: %s", code)
	}

	_, err = store.GetToken(ctx, "old_key")
	if err == nil {
		t.Errorf("expected error for deleted token")
	}
}
