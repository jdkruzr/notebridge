package sync

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/sysop/notebridge/internal/syncdb"
)

// handleCreateSummaryGroup handles POST /api/file/add/summary/group.
// Creates a summary group.
// Expects JSON body: {"uniqueIdentifier", "name", "description", "md5Hash", "creationTime", "lastModifiedTime"}
// Returns: {"cd": "000", "id": "snowflakeID"}
func (s *Server) handleCreateSummaryGroup(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract fields
	uniqueID := bodyStr(body, "uniqueIdentifier")
	if uniqueID == "" {
		jsonError(w, ErrBadRequest("missing or empty 'uniqueIdentifier' field"))
		return
	}

	name := bodyStr(body, "name")
	description := bodyStr(body, "description")
	md5Hash := bodyStr(body, "md5Hash")
	creationTime := bodyInt(body, "creationTime")
	lastModifiedTime := bodyInt(body, "lastModifiedTime")

	// Create summary with is_summary_group='Y'
	summary := &syncdb.Summary{
		ID:                       s.snowflake.Generate(),
		UserID:                   userID,
		UniqueIdentifier:         uniqueID,
		Name:                     name,
		Description:              description,
		MD5Hash:                  md5Hash,
		CreationTime:             creationTime,
		LastModifiedTime:         lastModifiedTime,
		IsSummaryGroup:           "Y",
		ParentUniqueIdentifier:   "",
		HandwriteInnerName:       "",
		CommentHandwriteName:     "",
		Metadata:                 "",
	}

	if err := s.store.CreateSummary(r.Context(), summary); err != nil {
		if errors.Is(err, syncdb.ErrUniqueIDExists) {
			jsonError(w, ErrUniqueIDExists())
			return
		}
		s.logger.Error("failed to create summary group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with id (Snowflake, as string)
	jsonSuccess(w, map[string]interface{}{
		"id": summary.ID,
	})
}

// handleUpdateSummaryGroup handles PUT /api/file/update/summary/group.
// Updates a summary group.
// Expects JSON body: {"id", partial update fields}
// Returns: {"cd": "000"}
func (s *Server) handleUpdateSummaryGroup(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract id
	id := bodyInt(body, "id")
	if id == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Build partial update map
	updates := make(map[string]interface{})
	if name := bodyStr(body, "name"); name != "" {
		updates["name"] = name
	}
	if description := bodyStr(body, "description"); description != "" {
		updates["description"] = description
	}
	if md5Hash := bodyStr(body, "md5Hash"); md5Hash != "" {
		updates["md5_hash"] = md5Hash
	}
	if lastModifiedTime := bodyInt(body, "lastModifiedTime"); lastModifiedTime != 0 {
		updates["last_modified_time"] = lastModifiedTime
	}

	if len(updates) == 0 {
		jsonError(w, ErrBadRequest("no update fields provided"))
		return
	}

	// Update summary
	if err := s.store.UpdateSummary(r.Context(), id, userID, updates); err != nil {
		if errors.Is(err, syncdb.ErrSummaryNotFound) {
			jsonError(w, ErrSummaryGroupNotFound())
			return
		}
		s.logger.Error("failed to update summary group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleDeleteSummaryGroup handles DELETE /api/file/delete/summary/group.
// Deletes a summary group.
// Expects JSON body: {"id"}
// Returns: {"cd": "000"}
func (s *Server) handleDeleteSummaryGroup(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract id
	id := bodyInt(body, "id")
	if id == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Delete summary
	if err := s.store.DeleteSummary(r.Context(), id, userID); err != nil {
		s.logger.Error("failed to delete summary group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleListSummaryGroups handles POST /api/file/query/summary/group.
// Lists summary groups for the user.
// Expects JSON body: {"page": 1, "size": 20}
// Returns: {"cd": "000", "totalRecords": N, "totalPages": M, "currentPage": 1, "pageSize": 20, "summaryDOList": [...]}
func (s *Server) handleListSummaryGroups(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract pagination parameters
	page := bodyInt(body, "page")
	if page <= 0 {
		page = 1
	}
	size := bodyInt(body, "size")
	if size <= 0 {
		size = 20
	}

	// List summary groups
	groups, total, err := s.store.ListSummaryGroups(r.Context(), userID, int(page), int(size))
	if err != nil {
		s.logger.Error("failed to list summary groups", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Calculate total pages
	totalPages := (total + int(size) - 1) / int(size)

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"totalRecords":  total,
		"totalPages":    totalPages,
		"currentPage":   page,
		"pageSize":      size,
		"summaryDOList": groups,
	})
}

// handleCreateSummary handles POST /api/file/add/summary.
// Creates a summary item.
// Expects JSON body: all summary fields
// Returns: {"cd": "000", "id": "snowflakeID"}
func (s *Server) handleCreateSummary(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract fields
	uniqueID := bodyStr(body, "uniqueIdentifier")
	if uniqueID == "" {
		jsonError(w, ErrBadRequest("missing or empty 'uniqueIdentifier' field"))
		return
	}

	summary := &syncdb.Summary{
		ID:                     s.snowflake.Generate(),
		UserID:                 userID,
		UniqueIdentifier:       uniqueID,
		Name:                   bodyStr(body, "name"),
		Description:            bodyStr(body, "description"),
		FileID:                 bodyInt(body, "fileId"),
		ParentUniqueIdentifier: bodyStr(body, "parentUniqueIdentifier"),
		Content:                bodyStr(body, "content"),
		DataSource:             bodyStr(body, "dataSource"),
		SourcePath:             bodyStr(body, "sourcePath"),
		SourceType:             bodyInt(body, "sourceType"),
		Tags:                   bodyStr(body, "tags"),
		MD5Hash:                bodyStr(body, "md5Hash"),
		HandwriteMD5:           bodyStr(body, "handwriteMD5"),
		HandwriteInnerName:     bodyStr(body, "handwriteInnerName"),
		Metadata:               bodyStr(body, "metadata"),
		CommentStr:             bodyStr(body, "commentStr"),
		CommentHandwriteName:   bodyStr(body, "commentHandwriteName"),
		IsSummaryGroup:         "N",
		Author:                 bodyStr(body, "author"),
		CreationTime:           bodyInt(body, "creationTime"),
		LastModifiedTime:       bodyInt(body, "lastModifiedTime"),
	}

	if err := s.store.CreateSummary(r.Context(), summary); err != nil {
		if errors.Is(err, syncdb.ErrUniqueIDExists) {
			jsonError(w, ErrUniqueIDExists())
			return
		}
		s.logger.Error("failed to create summary", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with id
	jsonSuccess(w, map[string]interface{}{
		"id": summary.ID,
	})
}

// handleUpdateSummary handles PUT /api/file/update/summary.
// Updates a summary item.
// Expects JSON body: {"id", partial update fields}
// Returns: {"cd": "000"}
func (s *Server) handleUpdateSummary(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract id
	id := bodyInt(body, "id")
	if id == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Build partial update map
	updates := make(map[string]interface{})
	if name := bodyStr(body, "name"); name != "" {
		updates["name"] = name
	}
	if description := bodyStr(body, "description"); description != "" {
		updates["description"] = description
	}
	if md5Hash := bodyStr(body, "md5Hash"); md5Hash != "" {
		updates["md5_hash"] = md5Hash
	}
	if lastModifiedTime := bodyInt(body, "lastModifiedTime"); lastModifiedTime != 0 {
		updates["last_modified_time"] = lastModifiedTime
	}
	if handwriteInnerName := bodyStr(body, "handwriteInnerName"); handwriteInnerName != "" {
		updates["handwrite_inner_name"] = handwriteInnerName
	}
	if metadata := bodyStr(body, "metadata"); metadata != "" {
		updates["metadata"] = metadata
	}

	if len(updates) == 0 {
		jsonError(w, ErrBadRequest("no update fields provided"))
		return
	}

	// Update summary
	if err := s.store.UpdateSummary(r.Context(), id, userID, updates); err != nil {
		if errors.Is(err, syncdb.ErrSummaryNotFound) {
			jsonError(w, ErrSummaryNotFound())
			return
		}
		s.logger.Error("failed to update summary", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleDeleteSummary handles DELETE /api/file/delete/summary.
// Deletes a summary item.
// Expects JSON body: {"id"}
// Returns: {"cd": "000"}
func (s *Server) handleDeleteSummary(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract id
	id := bodyInt(body, "id")
	if id == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Delete summary
	if err := s.store.DeleteSummary(r.Context(), id, userID); err != nil {
		s.logger.Error("failed to delete summary", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleQuerySummaryHash handles POST /api/file/query/summary/hash.
// Queries summary hashes (lightweight summary data).
// Expects JSON body: {"page": 1, "size": 20, "parentUniqueIdentifier": optional}
// Returns: {"cd": "000", "totalRecords": N, "totalPages": M, "summaryInfoVOList": [...]}
func (s *Server) handleQuerySummaryHash(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract pagination parameters
	page := bodyInt(body, "page")
	if page <= 0 {
		page = 1
	}
	size := bodyInt(body, "size")
	if size <= 0 {
		size = 20
	}

	// Extract optional parent UID filter
	var parentUID *string
	if pUID := bodyStr(body, "parentUniqueIdentifier"); pUID != "" {
		parentUID = &pUID
	}

	// List summary hashes
	hashes, total, err := s.store.ListSummaryHashes(r.Context(), userID, int(page), int(size), parentUID)
	if err != nil {
		s.logger.Error("failed to list summary hashes", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Calculate total pages
	totalPages := (total + int(size) - 1) / int(size)

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"totalRecords":     total,
		"totalPages":       totalPages,
		"summaryInfoVOList": hashes,
	})
}

// handleQuerySummaryByIDs handles POST /api/file/query/summary/id.
// Queries summaries by specific IDs.
// Expects JSON body: {"ids": [1, 2, 3], "page": 1, "size": 20}
// Returns: {"cd": "000", summaries with matching IDs}
func (s *Server) handleQuerySummaryByIDs(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract ids array
	idsRaw, ok := body["ids"].([]interface{})
	if !ok {
		jsonError(w, ErrBadRequest("missing or invalid 'ids' field"))
		return
	}

	var ids []int64
	for _, id := range idsRaw {
		switch v := id.(type) {
		case json.Number:
			if n, err := v.Int64(); err == nil {
				ids = append(ids, n)
			}
		case float64:
			ids = append(ids, int64(v))
		}
	}

	if len(ids) == 0 {
		jsonError(w, ErrBadRequest("no valid ids provided"))
		return
	}

	// Extract pagination parameters
	page := bodyInt(body, "page")
	if page <= 0 {
		page = 1
	}
	size := bodyInt(body, "size")
	if size <= 0 {
		size = 20
	}

	// Get summaries by IDs
	summaries, total, err := s.store.GetSummariesByIDs(r.Context(), userID, ids, int(page), int(size))
	if err != nil {
		s.logger.Error("failed to get summaries by IDs", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Calculate total pages
	totalPages := (total + int(size) - 1) / int(size)

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"totalRecords": total,
		"totalPages":   totalPages,
		"summaryDOList": summaries,
	})
}

// handleQuerySummaries handles POST /api/file/query/summary.
// Queries summaries with optional parent filter.
// Expects JSON body: {"page": 1, "size": 20, "parentUniqueIdentifier": optional}
// Returns: {"cd": "000", summaries matching criteria}
func (s *Server) handleQuerySummaries(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract pagination parameters
	page := bodyInt(body, "page")
	if page <= 0 {
		page = 1
	}
	size := bodyInt(body, "size")
	if size <= 0 {
		size = 20
	}

	// Extract optional parent UID filter
	var parentUID *string
	if pUID := bodyStr(body, "parentUniqueIdentifier"); pUID != "" {
		parentUID = &pUID
	}

	// List summaries
	summaries, total, err := s.store.ListSummaries(r.Context(), userID, int(page), int(size), parentUID)
	if err != nil {
		s.logger.Error("failed to list summaries", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Calculate total pages
	totalPages := (total + int(size) - 1) / int(size)

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"totalRecords": total,
		"totalPages":   totalPages,
		"summaryDOList": summaries,
	})
}

// handleUploadSummaryApply handles POST /api/file/upload/apply/summary.
// Generate signed upload URLs for summary file upload.
// Expects JSON body: {"fileName"}
// Returns: {"cd": "000", "fullUploadUrl": "...", "partUploadUrl": "...", "innerName": "..."}
func (s *Server) handleUploadSummaryApply(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract fileName
	fileName := bodyStr(body, "fileName")
	if fileName == "" {
		jsonError(w, ErrBadRequest("missing or empty 'fileName' field"))
		return
	}

	// Generate signed upload URLs (same pattern as file upload/apply)
	// Inner name is just the file name for simplicity
	innerName := fileName

	// Generate signed tokens
	uploadToken, err := s.authService.GenerateSignedURL(r.Context(), innerName, "upload", time.Hour)
	if err != nil {
		s.logger.Error("failed to generate upload URL", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	partUploadToken, err := s.authService.GenerateSignedURL(r.Context(), innerName, "upload_part", time.Hour)
	if err != nil {
		s.logger.Error("failed to generate upload URL", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Format as absolute URLs — the tablet follows these directly
	encodedPath := base64PathEncode("summaries/" + innerName)
	fullUploadURL := s.baseURL + "/api/oss/upload?signature=" + uploadToken + "&path=" + encodedPath
	partUploadURL := s.baseURL + "/api/oss/upload/part?signature=" + partUploadToken + "&path=" + encodedPath

	jsonSuccess(w, map[string]interface{}{
		"fullUploadUrl": fullUploadURL,
		"partUploadUrl": partUploadURL,
		"innerName":     innerName,
	})
}

// handleDownloadSummary handles POST /api/file/download/summary.
// Generate signed download URL for summary file download.
// Expects JSON body: {"id"}
// Returns: {"cd": "000", "url": "..."}
func (s *Server) handleDownloadSummary(w http.ResponseWriter, r *http.Request) {
	// Parse JSON body
	body, err := parseJSONBody(r)
	if err != nil {
		jsonError(w, ErrBadRequest("invalid JSON body"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract id
	id := bodyInt(body, "id")
	if id == 0 {
		jsonError(w, ErrBadRequest("missing or invalid 'id' field"))
		return
	}

	// Get summary from store
	summary, err := s.store.GetSummary(r.Context(), id, userID)
	if err != nil {
		s.logger.Error("failed to get summary", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if summary == nil {
		jsonError(w, ErrSummaryNotFound())
		return
	}

	// Check if handwrite_inner_name is set
	if summary.HandwriteInnerName == "" {
		jsonError(w, ErrSummaryNotFound())
		return
	}

	// Generate signed download token
	downloadToken, err := s.authService.GenerateSignedURL(r.Context(), summary.HandwriteInnerName, "download", time.Hour)
	if err != nil {
		s.logger.Error("failed to generate download URL", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Format as absolute URL
	downloadURL := s.baseURL + "/api/oss/download?signature=" + downloadToken + "&path=" + base64PathEncode("summaries/"+summary.HandwriteInnerName)

	jsonSuccess(w, map[string]interface{}{
		"url": downloadURL,
	})
}
