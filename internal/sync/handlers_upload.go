package sync

import (
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sysop/notebridge/internal/events"
	"github.com/sysop/notebridge/internal/syncdb"
)

// base64PathEncode encodes a path for use in query parameters
func base64PathEncode(path string) string {
	return url.QueryEscape(base64.StdEncoding.EncodeToString([]byte(path)))
}

// handleUploadApply handles POST /api/file/3/files/upload/apply.
// Generates signed upload URLs for the file.
// Expects JSON body: {"equipmentNo": "...", "path": "...", "fileName": "..."}
// Returns: {"cd": "000", "innerName": "...", "fullUploadUrl": "...", "partUploadUrl": "..."}
func (s *Server) handleUploadApply(w http.ResponseWriter, r *http.Request) {
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

	filePath := bodyStr(body, "path")
	fileName := bodyStr(body, "fileName")
	if fileName == "" {
		jsonError(w, ErrBadRequest("missing or empty 'fileName' field"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Generate signed tokens with 15-min TTL for uploads
	uploadToken, err := s.authService.GenerateSignedURL(r.Context(), filePath+"/"+fileName, "upload", 15*time.Minute)
	if err != nil {
		s.logger.Error("failed to generate upload token", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	partUploadToken, err := s.authService.GenerateSignedURL(r.Context(), filePath+"/"+fileName, "upload_part", 15*time.Minute)
	if err != nil {
		s.logger.Error("failed to generate part upload token", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Format as absolute URLs — the tablet follows these directly
	encodedPath := base64PathEncode(filePath + "/" + fileName)
	uploadURL := s.baseURL + "/api/oss/upload?signature=" + uploadToken + "&path=" + encodedPath
	partUploadURL := s.baseURL + "/api/oss/upload/part?signature=" + partUploadToken + "&path=" + encodedPath

	// Return success with URLs
	jsonSuccess(w, map[string]interface{}{
		"innerName":      fileName,
		"fullUploadUrl":  uploadURL,
		"partUploadUrl":  partUploadURL,
	})
}

// handleOssUpload handles POST /api/oss/upload.
// Uploads a complete file with signature verification.
// Expects: multipart form with "file" field, signature in query params
// Returns: 200 OK
func (s *Server) handleOssUpload(w http.ResponseWriter, r *http.Request) {
	// Verify signed URL
	signature := r.URL.Query().Get("signature")
	if signature == "" {
		jsonError(w, ErrBadRequest("missing signature"))
		return
	}

	pathStr := r.URL.Query().Get("path")
	if pathStr == "" {
		jsonError(w, ErrBadRequest("missing path"))
		return
	}

	// Decode path from base64
	pathBytes, err := base64.StdEncoding.DecodeString(pathStr)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid path encoding"))
		return
	}
	path := string(pathBytes)

	// Verify signature and consume nonce, check path matches
	signedPath, _, err := s.authService.VerifySignedURL(r.Context(), signature)
	if err != nil {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Verify the path in the signed URL matches the decoded path query param
	if signedPath != path {
		jsonError(w, ErrBadRequest("path mismatch with signed URL"))
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(100 * 1024 * 1024); err != nil { // 100MB max
		jsonError(w, ErrBadRequest("failed to parse multipart form"))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, ErrBadRequest("missing or invalid 'file' field"))
		return
	}
	defer file.Close()

	// Determine storage key from user email + path
	// For now, use path as key (simplified)
	storageKey := path

	// Upload to blob store
	_, md5hex, err := s.blobStore.Put(r.Context(), storageKey, file)
	if err != nil {
		s.logger.Error("failed to upload file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with MD5
	jsonSuccess(w, map[string]interface{}{"md5": md5hex})
}

// handleOssUploadPart handles POST /api/oss/upload/part.
// Uploads a chunk of a multi-part file.
// Expects: multipart form with "file", "partNumber", "totalChunks", "uploadId"
// Returns: {"cd": "000", "uploadId": "...", "partNumber": 1, "totalChunks": 3, "chunkMd5": "...", "status": "uploading"|"completed"}
func (s *Server) handleOssUploadPart(w http.ResponseWriter, r *http.Request) {
	// Verify signed URL
	signature := r.URL.Query().Get("signature")
	if signature == "" {
		jsonError(w, ErrBadRequest("missing signature"))
		return
	}

	// Verify signature and consume nonce
	_, _, err := s.authService.VerifySignedURL(r.Context(), signature)
	if err != nil {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(100 * 1024 * 1024); err != nil {
		jsonError(w, ErrBadRequest("failed to parse multipart form"))
		return
	}

	// Extract form fields
	file, _, err := r.FormFile("file")
	if err != nil {
		jsonError(w, ErrBadRequest("missing or invalid 'file' field"))
		return
	}
	defer file.Close()

	partNumberStr := r.FormValue("partNumber")
	totalChunksStr := r.FormValue("totalChunks")
	uploadID := r.FormValue("uploadId")

	if partNumberStr == "" || totalChunksStr == "" {
		jsonError(w, ErrBadRequest("missing partNumber or totalChunks"))
		return
	}

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid partNumber"))
		return
	}
	totalChunks, err := strconv.Atoi(totalChunksStr)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid totalChunks"))
		return
	}

	if uploadID == "" {
		uploadID = uuid.New().String()
	}

	// Save chunk
	chunkMd5, err := s.chunkStore.SaveChunk(uploadID, partNumber, file)
	if err != nil {
		s.logger.Error("failed to save chunk", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Record in DB
	if err := s.store.SaveChunkRecord(r.Context(), uploadID, partNumber, totalChunks, chunkMd5, ""); err != nil {
		s.logger.Error("failed to save chunk record", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Check if all chunks received
	count, err := s.store.CountChunks(r.Context(), uploadID)
	if err != nil {
		s.logger.Error("failed to count chunks", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	status := "uploading"
	if count >= totalChunks {
		status = "completed"
		// TODO: Merge chunks (will be done in upload/finish)
	}

	// Return success with chunk info
	jsonSuccess(w, map[string]interface{}{
		"uploadId":    uploadID,
		"partNumber":  partNumber,
		"totalChunks": totalChunks,
		"chunkMd5":    chunkMd5,
		"status":      status,
	})
}

// handleUploadFinish handles POST /api/file/2/files/upload/finish.
// Completes the upload and records file metadata.
// Expects JSON body: {"equipmentNo": "...", "path": "...", "fileName": "...", "content_hash": "...", "size": 12345}
// Returns: {"cd": "000", "path_display": "...", "id": "...", "size": 12345, "name": "...", "content_hash": "..."}
func (s *Server) handleUploadFinish(w http.ResponseWriter, r *http.Request) {
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

	filePath := strings.TrimPrefix(bodyStr(body, "path"), "/")
	fileName := bodyStr(body, "fileName")
	if fileName == "" {
		jsonError(w, ErrBadRequest("missing or empty 'fileName' field"))
		return
	}

	contentHash := bodyStr(body, "content_hash")
	size := bodyInt(body, "size")

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Resolve folder path to directory ID (creates intermediate folders if needed)
	dirPath := strings.TrimSuffix(filePath, "/")
	dirID := int64(0)
	if dirPath != "" {
		var resolveErr error
		dirID, resolveErr = s.resolvePathToDirectoryID(r.Context(), userID, dirPath)
		if resolveErr != nil {
			s.logger.Error("failed to resolve upload path", "path", dirPath, "error", resolveErr)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
	}

	// Check if file already exists in the target directory
	existing, err := s.store.GetFileByPath(r.Context(), userID, dirID, fileName)
	if err != nil {
		s.logger.Error("failed to check for existing file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	var fileID int64
	if existing != nil {
		// Update existing file
		fileID = existing.ID
		if err := s.store.UpdateFileMD5(r.Context(), fileID, contentHash, size); err != nil {
			s.logger.Error("failed to update file", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
	} else {
		// Create new file entry
		fileID = s.snowflake.Generate()
		// Storage key = clean relative path (no leading slash, no double slashes)
		storageKey := filePath + fileName
		entry := &syncdb.FileEntry{
			ID:          fileID,
			UserID:      userID,
			DirectoryID: dirID,
			FileName:    fileName,
			InnerName:   fileName,
			StorageKey:  storageKey,
			MD5:         contentHash,
			Size:        size,
			IsFolder:    false,
			IsActive:    true,
		}

		if err := s.store.CreateFile(r.Context(), entry); err != nil {
			s.logger.Error("failed to create file entry", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
	}

	// Refresh sync lock
	if err := s.store.RefreshLock(r.Context(), userID); err != nil {
		s.logger.Error("failed to refresh sync lock", "error", err)
		// Not fatal, continue
	}

	// Publish FileUploaded event
	eventPath := filePath + fileName
	s.eventBus.Publish(events.Event{
		Type:   events.FileUploaded,
		FileID: fileID,
		UserID: userID,
		Path:   eventPath,
	})

	// Return success with file metadata
	jsonSuccess(w, map[string]interface{}{
		"equipmentNo":  equipmentNo,
		"path_display": "/" + filePath + fileName,
		"id":           fileID,
		"size":         size,
		"name":         fileName,
		"content_hash": contentHash,
	})
}
