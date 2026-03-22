package sync

import (
	"encoding/base64"
	"net/http"
	"os"
	"time"
)

// handleDownloadV3 handles POST /api/file/3/files/download_v3.
// Generates a signed download URL for a file.
// Expects JSON body: {"equipmentNo": "...", "id": 12345}
// Returns: {"cd": "000", "id": 12345, "url": "...", "name": "...", "path_display": "...", "content_hash": "...", "size": 12345, "is_downloadable": true}
func (s *Server) handleDownloadV3(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Extract fields
	equipmentNo := bodyStr(body, "equipmentNo")
	if equipmentNo == "" {
		jsonError(w, ErrBadRequest("missing or empty 'equipmentNo' field"))
		return
	}

	fileID := bodyInt(body, "id")
	if fileID == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Get file entry
	file, err := s.store.GetFile(r.Context(), fileID, userID)
	if err != nil {
		s.logger.Error("failed to get file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if file == nil || file.IsFolder {
		jsonError(w, ErrBadRequest("file not found or is a folder"))
		return
	}

	// Verify file exists on disk
	if !s.blobStore.Exists(r.Context(), file.StorageKey) {
		jsonError(w, ErrBadRequest("file not found on disk"))
		return
	}

	// Generate signed download URL with 24-hr TTL
	downloadURL, err := s.authService.GenerateSignedURL(r.Context(), file.StorageKey, "download", 24*time.Hour)
	if err != nil {
		s.logger.Error("failed to generate download URL", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with download metadata
	jsonSuccess(w, map[string]interface{}{
		"id":              fileID,
		"url":             downloadURL,
		"name":            file.FileName,
		"path_display":    "/" + file.FileName,
		"content_hash":    file.MD5,
		"size":            file.Size,
		"is_downloadable": true,
	})
}

// handleOssDownload handles GET /api/oss/download.
// Downloads a file with signature verification and Range header support.
// Expects: signature in query params, path in query params (base64 encoded)
// Returns: 200 OK with file content or 206 Partial Content for Range requests
func (s *Server) handleOssDownload(w http.ResponseWriter, r *http.Request) {
	// Verify signed URL
	signature := r.URL.Query().Get("signature")
	if signature == "" {
		http.Error(w, "missing signature", http.StatusBadRequest)
		return
	}

	pathStr := r.URL.Query().Get("path")
	if pathStr == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}

	// Decode path from base64
	pathBytes, err := base64.StdEncoding.DecodeString(pathStr)
	if err != nil {
		http.Error(w, "invalid path encoding", http.StatusBadRequest)
		return
	}
	path := string(pathBytes)

	// Verify signature and consume nonce
	_, _, err = s.authService.VerifySignedURL(r.Context(), signature)
	if err != nil {
		http.Error(w, "invalid or expired signature", http.StatusUnauthorized)
		return
	}

	// Get absolute path from blob store
	absolutePath := s.blobStore.Path(path)

	// Open file (needed for ServeContent to detect size and support Range)
	file, err := os.Open(absolutePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			s.logger.Error("failed to open file for download", "error", err)
		}
		return
	}
	defer file.Close()

	// Get file info for modtime
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		s.logger.Error("failed to stat file", "error", err)
		return
	}

	// Use http.ServeContent to handle Range requests automatically
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
