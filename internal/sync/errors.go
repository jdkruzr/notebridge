package sync

import (
	"fmt"
	"net/http"
)

// SyncError represents an SPC-compatible error response.
// Error codes match what the Supernote tablet firmware expects.
type SyncError struct {
	Code       string
	Message    string
	HTTPStatus int
}

// Error implements the error interface.
func (e *SyncError) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// ErrWrongPassword returns an E0019 error (wrong password).
func ErrWrongPassword() *SyncError {
	return &SyncError{
		Code:       "E0019",
		Message:    "wrong password",
		HTTPStatus: http.StatusUnauthorized,
	}
}

// ErrAccountLocked returns an E0045 error (account locked - too many failures).
func ErrAccountLocked() *SyncError {
	return &SyncError{
		Code:       "E0045",
		Message:    "account locked",
		HTTPStatus: http.StatusForbidden,
	}
}

// ErrInvalidToken returns an E0712 error (invalid/expired token).
func ErrInvalidToken() *SyncError {
	return &SyncError{
		Code:       "E0712",
		Message:    "invalid or expired token",
		HTTPStatus: http.StatusUnauthorized,
	}
}

// ErrSyncLocked returns an E0078 error (sync lock held by another device).
func ErrSyncLocked() *SyncError {
	return &SyncError{
		Code:       "E0078",
		Message:    "sync locked by another device",
		HTTPStatus: http.StatusLocked,
	}
}

// ErrBadRequest returns an E0018 error (invalid request / missing parameters).
func ErrBadRequest(msg string) *SyncError {
	return &SyncError{
		Code:       "E0018",
		Message:    msg,
		HTTPStatus: http.StatusBadRequest,
	}
}

// ErrInternal returns an E9999 error (internal server error).
func ErrInternal(msg string) *SyncError {
	return &SyncError{
		Code:       "E9999",
		Message:    msg,
		HTTPStatus: http.StatusInternalServerError,
	}
}

// ErrCircularMove returns an E0358 error (circular move detection).
func ErrCircularMove() *SyncError {
	return &SyncError{
		Code:       "E0358",
		Message:    "cannot move folder into itself",
		HTTPStatus: http.StatusBadRequest,
	}
}

// ErrNameCollision returns an E0322 error (file name collision).
func ErrNameCollision() *SyncError {
	return &SyncError{
		Code:       "E0322",
		Message:    "file already exists at destination",
		HTTPStatus: http.StatusConflict,
	}
}
