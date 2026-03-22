package sync

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sysop/notebridge/internal/syncdb"
)

// AuthService handles authentication operations.
type AuthService struct {
	store     *syncdb.Store
	jwtSecret []byte
	snowflake *SnowflakeGenerator
}

// NewAuthService creates a new AuthService instance.
func NewAuthService(store *syncdb.Store, snowflake *SnowflakeGenerator) *AuthService {
	return &AuthService{
		store:     store,
		snowflake: snowflake,
	}
}

// GenerateChallenge generates an 8-char random alphanumeric code and stores it.
func (a *AuthService) GenerateChallenge(ctx context.Context, account string) (string, int64, error) {
	// Generate 8-char random alphanumeric code
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	code := make([]byte, 8)
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", 0, fmt.Errorf("failed to generate random code: %w", err)
	}
	for i := 0; i < 8; i++ {
		code[i] = charset[int(randomBytes[i])%len(charset)]
	}

	// Generate unique timestamp - if collision, sleep and retry
	var timestamp int64
	for {
		timestamp = time.Now().Unix()
		// Delete any old challenges for this account (cleanup)
		_ = a.store.DeleteChallenge(ctx, account, timestamp)
		// Try to create
		err := a.store.CreateChallenge(ctx, account, string(code), timestamp)
		if err == nil {
			break // Success
		}
		// If duplicate key, sleep 1ms and retry with next timestamp
		time.Sleep(1 * time.Millisecond)
	}

	return string(code), timestamp, nil
}

// VerifyLogin verifies the login attempt using challenge-response flow.
// submittedHash is SHA256(user.PasswordHash + challenge.RandomCode) as hex.
func (a *AuthService) VerifyLogin(ctx context.Context, account, submittedHash string, timestamp int64) (string, error) {
	// Look up user
	user, err := a.store.GetUserByEmail(ctx, account)
	if err != nil {
		return "", fmt.Errorf("failed to get user: %w", err)
	}
	if user == nil {
		return "", ErrWrongPassword()
	}

	// Check account lockout
	if user.LockedUntil != nil && time.Now().Before(*user.LockedUntil) {
		return "", ErrAccountLocked()
	}

	// Look up challenge
	randomCode, err := a.store.GetChallenge(ctx, account, timestamp)
	if err != nil {
		return "", ErrWrongPassword()
	}

	// Check challenge age (must be within 5 minutes)
	now := time.Now().Unix()
	if now-timestamp > 5*60 {
		return "", ErrWrongPassword() // AC1.6: expired challenge
	}

	// Compute expected hash
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(user.PasswordHash+randomCode)))

	// Compare using constant-time comparison to prevent timing attacks
	if subtle.ConstantTimeCompare([]byte(submittedHash), []byte(expectedHash)) != 1 {
		// Wrong password
		if err := a.store.IncrementErrorCount(ctx, user.ID); err != nil {
			return "", fmt.Errorf("failed to increment error count: %w", err)
		}

		// Check if should lock account (6 errors in 12 hours)
		user, _ = a.store.GetUserByEmail(ctx, account)
		if user.ErrorCount >= 6 {
			lockUntil := time.Now().Add(5 * time.Minute)
			if err := a.store.LockUser(ctx, user.ID, lockUntil); err != nil {
				return "", fmt.Errorf("failed to lock user: %w", err)
			}
			return "", ErrAccountLocked()
		}

		return "", ErrWrongPassword() // AC1.3: wrong password
	}

	// Password correct - reset error count
	if err := a.store.ResetErrorCount(ctx, user.ID); err != nil {
		return "", fmt.Errorf("failed to reset error count: %w", err)
	}

	// Create JWT token
	token, err := a.createJWTToken(ctx, user.ID, "")
	if err != nil {
		return "", fmt.Errorf("failed to create JWT token: %w", err)
	}

	// Delete challenge (single-use)
	_ = a.store.DeleteChallenge(ctx, account, timestamp)

	return token, nil
}

// createJWTToken creates a JWT token with the given claims.
func (a *AuthService) createJWTToken(ctx context.Context, userID int64, equipmentNo string) (string, error) {
	// Get or create JWT secret
	if a.jwtSecret == nil {
		secret, err := a.store.GetOrCreateJWTSecret(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get jwt secret: %w", err)
		}
		a.jwtSecret = []byte(secret)
	}

	// Create a random JTI (key for DB lookup)
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", fmt.Errorf("failed to generate JTI: %w", err)
	}
	jti := hex.EncodeToString(jtiBytes)

	// Create claims
	claims := jwt.MapClaims{
		"sub":        strconv.FormatInt(userID, 10),
		"equipmentNo": equipmentNo,
		"iat":        time.Now().Unix(),
		"exp":        time.Now().Add(30 * 24 * time.Hour).Unix(),
		"jti":        jti,
	}

	// Sign token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	// Store in auth_tokens
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if err := a.store.StoreToken(ctx, jti, tokenStr, userID, equipmentNo, expiresAt); err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return tokenStr, nil
}

// ValidateJWTToken verifies a JWT token and checks it exists in the database.
func (a *AuthService) ValidateJWTToken(ctx context.Context, tokenString string) (int64, string, error) {
	// Get or create JWT secret
	if a.jwtSecret == nil {
		secret, err := a.store.GetOrCreateJWTSecret(ctx)
		if err != nil {
			return 0, "", fmt.Errorf("failed to get jwt secret: %w", err)
		}
		a.jwtSecret = []byte(secret)
	}

	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	})

	if err != nil {
		return 0, "", ErrInvalidToken()
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return 0, "", ErrInvalidToken() // AC1.4: invalid token
	}

	// Extract claims
	subStr, ok := claims["sub"].(string)
	if !ok {
		return 0, "", ErrInvalidToken()
	}

	userID, err := strconv.ParseInt(subStr, 10, 64)
	if err != nil {
		return 0, "", ErrInvalidToken()
	}

	equipmentNo, _ := claims["equipmentNo"].(string)
	jti, _ := claims["jti"].(string)

	// Verify token exists in database (not revoked)
	dbToken, err := a.store.GetToken(ctx, jti)
	if err != nil {
		return 0, "", ErrInvalidToken() // AC1.4: token not in DB or expired
	}

	if dbToken.UserID != userID {
		return 0, "", ErrInvalidToken()
	}

	return userID, equipmentNo, nil
}

// GenerateSignedURL creates a JWT token for a signed URL with single-use nonce.
func (a *AuthService) GenerateSignedURL(ctx context.Context, path, action string, ttl time.Duration) (string, error) {
	// Get or create JWT secret
	if a.jwtSecret == nil {
		secret, err := a.store.GetOrCreateJWTSecret(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get jwt secret: %w", err)
		}
		a.jwtSecret = []byte(secret)
	}

	// Generate random nonce
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	// Create claims
	expiresAt := time.Now().Add(ttl)
	claims := jwt.MapClaims{
		"path":   path,
		"action": action,
		"nonce":  nonce,
		"iat":    time.Now().Unix(),
		"exp":    expiresAt.Unix(),
	}

	// Sign token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	// Store nonce
	if err := a.store.StoreNonce(ctx, nonce, expiresAt); err != nil {
		return "", fmt.Errorf("failed to store nonce: %w", err)
	}

	return tokenStr, nil
}

// VerifySignedURL verifies a signed URL token and consumes the nonce (single-use).
func (a *AuthService) VerifySignedURL(ctx context.Context, tokenString string) (string, string, error) {
	// Get or create JWT secret
	if a.jwtSecret == nil {
		secret, err := a.store.GetOrCreateJWTSecret(ctx)
		if err != nil {
			return "", "", fmt.Errorf("failed to get jwt secret: %w", err)
		}
		a.jwtSecret = []byte(secret)
	}

	// Parse token
	token, err := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.jwtSecret, nil
	})

	if err != nil {
		return "", "", ErrInvalidToken()
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", "", ErrInvalidToken()
	}

	// Extract claims
	path, ok := claims["path"].(string)
	if !ok {
		return "", "", ErrInvalidToken()
	}

	action, ok := claims["action"].(string)
	if !ok {
		return "", "", ErrInvalidToken()
	}

	nonce, ok := claims["nonce"].(string)
	if !ok {
		return "", "", ErrInvalidToken()
	}

	// Consume nonce (single-use)
	consumed, err := a.store.ConsumeNonce(ctx, nonce)
	if err != nil {
		return "", "", fmt.Errorf("failed to consume nonce: %w", err)
	}

	if !consumed {
		return "", "", ErrInvalidToken() // Nonce already used or expired
	}

	return path, action, nil
}
