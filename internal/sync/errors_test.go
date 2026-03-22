package sync

import (
	"net/http"
	"testing"
)

func TestErrWrongPassword(t *testing.T) {
	t.Helper()

	err := ErrWrongPassword()

	if err.Code != "E0019" {
		t.Errorf("expected code E0019, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", err.HTTPStatus)
	}
	if err.Message == "" {
		t.Errorf("expected non-empty message")
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestErrAccountLocked(t *testing.T) {
	t.Helper()

	err := ErrAccountLocked()

	if err.Code != "E0045" {
		t.Errorf("expected code E0045, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusForbidden {
		t.Errorf("expected HTTP 403, got %d", err.HTTPStatus)
	}
	if err.Message == "" {
		t.Errorf("expected non-empty message")
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestErrInvalidToken(t *testing.T) {
	t.Helper()

	err := ErrInvalidToken()

	if err.Code != "E0712" {
		t.Errorf("expected code E0712, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("expected HTTP 401, got %d", err.HTTPStatus)
	}
	if err.Message == "" {
		t.Errorf("expected non-empty message")
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestErrSyncLocked(t *testing.T) {
	t.Helper()

	err := ErrSyncLocked()

	if err.Code != "E0078" {
		t.Errorf("expected code E0078, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusLocked {
		t.Errorf("expected HTTP 423, got %d", err.HTTPStatus)
	}
	if err.Message == "" {
		t.Errorf("expected non-empty message")
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestErrBadRequest(t *testing.T) {
	t.Helper()

	msg := "missing parameter"
	err := ErrBadRequest(msg)

	if err.Code != "E0018" {
		t.Errorf("expected code E0018, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusBadRequest {
		t.Errorf("expected HTTP 400, got %d", err.HTTPStatus)
	}
	if err.Message != msg {
		t.Errorf("expected message %q, got %q", msg, err.Message)
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestErrInternal(t *testing.T) {
	t.Helper()

	msg := "database connection failed"
	err := ErrInternal(msg)

	if err.Code != "E9999" {
		t.Errorf("expected code E9999, got %s", err.Code)
	}
	if err.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("expected HTTP 500, got %d", err.HTTPStatus)
	}
	if err.Message != msg {
		t.Errorf("expected message %q, got %q", msg, err.Message)
	}
	if err.Error() == "" {
		t.Errorf("expected non-empty Error() string")
	}
}

func TestSyncErrorImplementsErrorInterface(t *testing.T) {
	t.Helper()

	var err error = ErrWrongPassword()
	if err == nil {
		t.Errorf("expected SyncError to implement error interface")
	}
}

func TestSyncErrorErrorMethod(t *testing.T) {
	t.Helper()

	tests := []struct {
		name     string
		err      *SyncError
		contains string
	}{
		{
			name:     "wrong password",
			err:      ErrWrongPassword(),
			contains: "E0019",
		},
		{
			name:     "account locked",
			err:      ErrAccountLocked(),
			contains: "E0045",
		},
		{
			name:     "invalid token",
			err:      ErrInvalidToken(),
			contains: "E0712",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if errStr == "" {
				t.Errorf("Error() should return non-empty string")
			}
			// Error message should contain the code
			if !contains(errStr, tt.contains) {
				t.Errorf("Error() %q should contain %q", errStr, tt.contains)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
