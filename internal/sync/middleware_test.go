package sync

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sysop/notebridge/internal/syncdb"
)

func TestAuthMiddlewareValidToken(t *testing.T) {
	// Setup
	db, err := syncdb.Open(":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	store := syncdb.NewStore(db)
	sfg := NewSnowflakeGenerator()
	authService := NewAuthService(store, sfg)
	ctx := context.Background()

	// Create user and get valid token
	passwordHash := "md5hash"
	err = store.EnsureUser(ctx, "test@example.com", passwordHash, 1000000000000001)
	if err != nil {
		t.Fatalf("failed to ensure user: %v", err)
	}

	user, _ := store.GetUserByEmail(ctx, "test@example.com")

	// Generate valid token
	randomCode, timestamp, _ := authService.GenerateChallenge(ctx, "test@example.com")
	expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(passwordHash+randomCode)))
	tokenStr, _ := authService.VerifyLogin(ctx, "test@example.com", expectedHash, timestamp)

	// Create middleware
	middleware := AuthMiddleware(authService)

	// Create test handler
	nextCalled := false
	var gotUserID int64
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		gotUserID = UserIDFromContext(r.Context())
		_ = EquipmentNoFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Make request with valid token
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// AC1.2: Valid Bearer token accepted, context populated
	if !nextCalled {
		t.Fatal("next handler not called")
	}
	if gotUserID != user.ID {
		t.Errorf("expected userID %d, got %d", user.ID, gotUserID)
	}

	t.Logf("AC1.2 PASS: Valid Bearer token accepted, context has userID=%d", gotUserID)
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	// Setup
	db, _ := syncdb.Open(":memory:")
	defer db.Close()

	store := syncdb.NewStore(db)
	sfg := NewSnowflakeGenerator()
	authService := NewAuthService(store, sfg)

	middleware := AuthMiddleware(authService)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Make request without Authorization header
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// AC1.4: Missing header returns 401 with E0712
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if code, ok := response["cd"].(string); ok {
		if code != "E0712" {
			t.Errorf("expected E0712, got %s", code)
		}
	} else {
		t.Fatal("no error code in response")
	}

	t.Logf("AC1.4 PASS: Missing header returns 401 with E0712")
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	db, _ := syncdb.Open(":memory:")
	defer db.Close()

	store := syncdb.NewStore(db)
	sfg := NewSnowflakeGenerator()
	authService := NewAuthService(store, sfg)

	middleware := AuthMiddleware(authService)

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware(nextHandler)

	// Make request with invalid token
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// AC1.4: Invalid token returns 401 with E0712
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if code, ok := response["cd"].(string); ok {
		if code != "E0712" {
			t.Errorf("expected E0712, got %s", code)
		}
	} else {
		t.Fatal("no error code in response")
	}

	t.Logf("AC1.4 PASS: Invalid token returns 401 with E0712")
}

func TestRecoveryMiddleware(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	middleware := RecoveryMiddleware(logger)

	// Create handler that panics
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})

	handler := middleware(panicHandler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Should return 500, not crash
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", w.Code)
	}

	// Check response has error code
	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	if code, ok := response["cd"].(string); ok {
		if code != "E9999" {
			t.Errorf("expected E9999, got %s", code)
		}
	}

	// Should have logged the panic
	logOutput := logBuffer.String()
	if logOutput == "" {
		t.Error("expected log output")
	}

	t.Logf("RecoveryMiddleware PASS: Handler panic recovered, 500 returned")
}

func TestLoggingMiddleware(t *testing.T) {
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))

	middleware := LoggingMiddleware(logger)

	// Create test handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Add slight delay
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler := middleware(nextHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Check status code
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	// Should have logged request
	logOutput := logBuffer.String()
	if logOutput == "" {
		t.Error("expected log output")
	}

	// Should contain method, path, status
	if !containsStr(logOutput, "GET") || !containsStr(logOutput, "/api/test") || !containsStr(logOutput, "200") {
		t.Errorf("log missing expected fields: %s", logOutput)
	}

	t.Logf("LoggingMiddleware PASS: Request logged with method, path, status")
}

// Helper function (note: there's another 'contains' in errors_test.go, but we need it here too for middleware tests)
func containsStr(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func TestUserIDFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKeyUserID, int64(42))
	userID := UserIDFromContext(ctx)
	if userID != 42 {
		t.Errorf("expected userID 42, got %d", userID)
	}
}

func TestEquipmentNoFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), contextKeyEquipmentNo, "device123")
	equipmentNo := EquipmentNoFromContext(ctx)
	if equipmentNo != "device123" {
		t.Errorf("expected device123, got %s", equipmentNo)
	}
}
