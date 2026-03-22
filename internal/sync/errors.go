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

// ErrTaskGroupNotFound returns an E0328 error (task group not found).
func ErrTaskGroupNotFound() *SyncError {
	return &SyncError{
		Code:       "E0328",
		Message:    "task group not found",
		HTTPStatus: http.StatusNotFound,
	}
}

// ErrTaskNotFound returns an E0329 error (task not found).
func ErrTaskNotFound() *SyncError {
	return &SyncError{
		Code:       "E0329",
		Message:    "task not found",
		HTTPStatus: http.StatusNotFound,
	}
}

// ErrUniqueIDExists returns an E0338 error (summary unique ID already exists).
func ErrUniqueIDExists() *SyncError {
	return &SyncError{
		Code:       "E0338",
		Message:    "summary unique ID already exists",
		HTTPStatus: http.StatusConflict,
	}
}

// ErrSummaryGroupNotFound returns an E0339 error (summary group not found).
func ErrSummaryGroupNotFound() *SyncError {
	return &SyncError{
		Code:       "E0339",
		Message:    "summary group not found",
		HTTPStatus: http.StatusNotFound,
	}
}

// ErrSummaryNotFound returns an E0340 error (summary not found).
func ErrSummaryNotFound() *SyncError {
	return &SyncError{
		Code:       "E0340",
		Message:    "summary not found",
		HTTPStatus: http.StatusNotFound,
	}
}
