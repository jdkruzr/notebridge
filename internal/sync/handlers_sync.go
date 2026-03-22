package sync

import (
	"net/http"
)

// handleSyncStart handles POST /api/file/2/files/synchronous/start.
// Acquires a sync lock for the device.
// Expects JSON body: {"equipmentNo": "..."}
// Returns: {"cd": "000", "equipmentNo": "...", "synType": "incremental"}
func (s *Server) handleSyncStart(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Extract equipmentNo
	equipmentNo := bodyStr(body, "equipmentNo")
	if equipmentNo == "" {
		jsonError(w, ErrBadRequest("missing or empty 'equipmentNo' field"))
		return
	}

	// Get userID from context (set by auth middleware)
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Attempt to acquire sync lock
	err = s.store.AcquireLock(r.Context(), userID, equipmentNo)
	if err != nil {
		// Check if it's a sync lock conflict
		if err.Error() == "sync locked by another device" {
			jsonError(w, ErrSyncLocked())
			return
		}
		s.logger.Error("failed to acquire sync lock", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with equipment details
	jsonSuccess(w, map[string]interface{}{
		"equipmentNo": equipmentNo,
		"synType":     "incremental",
	})
}

// handleSyncEnd handles POST /api/file/2/files/synchronous/end.
// Releases the sync lock for the device.
// Expects JSON body: {"equipmentNo": "..."}
// Returns: {"cd": "000"}
func (s *Server) handleSyncEnd(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Extract equipmentNo
	equipmentNo := bodyStr(body, "equipmentNo")
	if equipmentNo == "" {
		jsonError(w, ErrBadRequest("missing or empty 'equipmentNo' field"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Release sync lock
	err = s.store.ReleaseLock(r.Context(), userID, equipmentNo)
	if err != nil {
		s.logger.Error("failed to release sync lock", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}
