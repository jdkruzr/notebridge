package sync

import (
	"net/http"
)

// handleChallenge handles POST /api/user/login/challenge.
// Expects JSON body: {"account": "user@email.com"}
// Returns: {"cd": "000", "randomCode": "...", "timestamp": 12345}
func (s *Server) handleChallenge(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Extract account
	account := bodyStr(body, "account")
	if account == "" {
		jsonError(w, ErrBadRequest("missing or empty 'account' field"))
		return
	}

	// Generate challenge
	randomCode, timestamp, err := s.authService.GenerateChallenge(r.Context(), account)
	if err != nil {
		s.logger.Error("failed to generate challenge", "error", err)
		jsonError(w, ErrInternal(err.Error()))
		return
	}

	// Return success with challenge details
	jsonSuccess(w, map[string]interface{}{
		"randomCode": randomCode,
		"timestamp":  timestamp,
	})
}

// handleLoginVerify handles POST /api/user/login/verify.
// Expects JSON body: {"account": "...", "password": "SHA256_hash", "timestamp": 12345, "equipmentNo": "SN100..."}
// Returns: {"cd": "000", "token": "...", "user": {...}}
func (s *Server) handleLoginVerify(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Extract fields
	account := bodyStr(body, "account")
	if account == "" {
		jsonError(w, ErrBadRequest("missing or empty 'account' field"))
		return
	}

	submittedHash := bodyStr(body, "password")
	if submittedHash == "" {
		jsonError(w, ErrBadRequest("missing or empty 'password' field"))
		return
	}

	timestamp := bodyInt(body, "timestamp")
	if timestamp == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'timestamp' field"))
		return
	}

	equipmentNo := bodyStr(body, "equipmentNo")
	if equipmentNo == "" {
		jsonError(w, ErrBadRequest("missing or empty 'equipmentNo' field"))
		return
	}

	// Verify login
	token, err := s.authService.VerifyLogin(r.Context(), account, submittedHash, timestamp)
	if err != nil {
		// Check if it's a SyncError
		syncErr, ok := err.(*SyncError)
		if ok {
			jsonError(w, syncErr)
		} else {
			s.logger.Error("failed to verify login", "error", err)
			jsonError(w, ErrInternal(err.Error()))
		}
		return
	}

	// Ensure equipment record exists
	user, err := s.store.GetUserByEmail(r.Context(), account)
	if err != nil {
		s.logger.Error("failed to get user after successful login", "error", err)
		jsonError(w, ErrInternal(err.Error()))
		return
	}

	if err := s.store.EnsureEquipment(r.Context(), equipmentNo, user.ID); err != nil {
		s.logger.Error("failed to ensure equipment record", "error", err)
		jsonError(w, ErrInternal(err.Error()))
		return
	}

	// Return success with token and user info
	jsonSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"account": account,
		},
	})
}

// handleHealth handles GET /health.
// Returns: {"status": "ok"} with 200 OK
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{
		"status": "ok",
	})
}
