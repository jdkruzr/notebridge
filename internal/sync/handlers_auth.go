package sync

import (
	"net/http"
	"strconv"
	"syscall"
	"time"

	"github.com/sysop/notebridge/internal/blob"
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
		jsonError(w, ErrInternal("internal server error"))
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
			jsonError(w, ErrInternal("internal server error"))
		}
		return
	}

	// Ensure equipment record exists
	user, err := s.store.GetUserByEmail(r.Context(), account)
	if err != nil {
		s.logger.Error("failed to get user after successful login", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if err := s.store.EnsureEquipment(r.Context(), equipmentNo, user.ID); err != nil {
		s.logger.Error("failed to ensure equipment record", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success matching SPC response format
	jsonSuccess(w, map[string]interface{}{
		"token":           token,
		"counts":          "0",
		"userName":        account,
		"avatarsUrl":      "",
		"lastUpdateTime":  time.Now().Format("2006-01-02 15:04:05"),
		"isBind":          "Y",
		"isBindEquipment": "Y",
		"soldOutCount":    0,
	})
}

// handleQueryServer handles GET /api/file/query/server.
// Pre-auth connectivity check — tablet calls this first to verify server is reachable.
// Returns: {"cd": "000"} (simple success, no data)
func (s *Server) handleQueryServer(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, nil)
}

// handleCheckUserExists handles POST /api/official/user/check/exists/server.
// Tablet checks if user account exists before starting auth flow.
func (s *Server) handleCheckUserExists(w http.ResponseWriter, r *http.Request) {
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	email := bodyStr(body, "email")
	user, _ := s.store.GetUserByEmail(r.Context(), email)
	if user == nil {
		jsonSuccess(w, map[string]interface{}{"userId": 0, "dms": "ALL", "uniqueMachineId": s.machineID, "errorCode": nil, "errorMsg": nil})
		return
	}
	jsonSuccess(w, map[string]interface{}{"userId": user.ID, "dms": "ALL", "uniqueMachineId": s.machineID, "errorCode": nil, "errorMsg": nil})
}

// handleBindEquipment handles POST /api/terminal/user/bindEquipment.
// Registers device metadata (name, capacity) after login.
func (s *Server) handleBindEquipment(w http.ResponseWriter, r *http.Request) {
	body, _ := parseJSONBody(r)
	equipmentNo := bodyStr(body, "equipmentNo")
	if equipmentNo != "" {
		userID := UserIDFromContext(r.Context())
		if userID != 0 {
			_ = s.store.EnsureEquipment(r.Context(), equipmentNo, userID)
		}
	}
	jsonSuccess(w, nil)
}

// handleUnbindEquipment handles POST /api/terminal/equipment/unlink.
func (s *Server) handleUnbindEquipment(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, nil)
}

// handleUserQuery handles POST /api/user/query.
// Returns user profile info. Tablet calls this after login.
func (s *Server) handleUserQuery(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	user, err := s.store.GetUserByID(r.Context(), userID)
	if err != nil || user == nil {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Report actual disk capacity
	totalCapacity := strconv.FormatInt(diskTotalBytes(s.blobStore), 10)

	jsonSuccess(w, map[string]interface{}{
		"address": "", "avatarsUrl": "", "birthday": "", "education": "",
		"email": user.Email, "hobby": "", "job": "", "personalSign": "",
		"telephone": "", "countryCode": "", "sex": "",
		"totalCapacity": totalCapacity,
		"userName":      user.Email,
		"fileServer":    "",
		"userId":        user.ID,
	})
}

// handleUserLogout handles POST /api/user/logout.
func (s *Server) handleUserLogout(w http.ResponseWriter, r *http.Request) {
	// Token revocation could be added here
	jsonSuccess(w, nil)
}

// diskTotalBytes returns the total filesystem capacity in bytes for the blob store root.
func diskTotalBytes(store blob.BlobStore) int64 {
	root := store.Path("")
	var stat syscall.Statfs_t
	if syscall.Statfs(root, &stat) != nil {
		return 0
	}
	return int64(stat.Blocks) * int64(stat.Bsize)
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{
		"status": "ok",
	})
}
