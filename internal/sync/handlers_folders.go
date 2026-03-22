package sync

import (
	"net/http"
	"path"

	"github.com/sysop/notebridge/internal/syncdb"
)

// handleCreateFolder handles POST /api/file/2/files/create_folder_v2.
// Creates a folder in the file catalog.
// Expects JSON body: {"equipmentNo": "...", "path": "/Folder", "autorename": true|false}
// Returns: {"cd": "000", "tag": "folder", "id": "...", "name": "...", "path_display": "..."}
func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
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

	folderPath := bodyStr(body, "path")
	if folderPath == "" {
		jsonError(w, ErrBadRequest("missing or empty 'path' field"))
		return
	}

	autorename := bodyBool(body, "autorename")

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Parse path to get parent directory ID and folder name
	folderName := path.Base(folderPath)
	parentPath := path.Dir(folderPath)

	// Resolve parent directory ID (0 = root)
	parentID := int64(0)
	if parentPath != "/" && parentPath != "." {
		// TODO: Resolve path to directory ID (for now, assume root)
		// In a real implementation, we'd walk the path and find or create intermediate folders
		parentID = 0
	}

	// Check for name collision
	existing, err := s.store.GetFileByPath(r.Context(), userID, parentID, folderName)
	if err != nil {
		s.logger.Error("failed to check for existing folder", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	var finalName string
	if existing != nil {
		if !autorename {
			// Collision and autorename is disabled
			jsonError(w, ErrBadRequest("folder already exists"))
			return
		}
		// Autorename needed
		existingNames, err := s.store.FindByName(r.Context(), userID, parentID, folderName)
		if err != nil {
			s.logger.Error("failed to find existing names", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
		finalName = AutoRename(folderName, existingNames)
	} else {
		finalName = folderName
	}

	// Create folder entry with Snowflake ID
	folderID := s.snowflake.Generate()
	entry := &syncdb.FileEntry{
		ID:          folderID,
		UserID:      userID,
		DirectoryID: parentID,
		FileName:    finalName,
		InnerName:   finalName,
		StorageKey:  "", // Folders have no storage key
		MD5:         "",
		Size:        0,
		IsFolder:    true,
		IsActive:    true,
	}

	if err := s.store.CreateFile(r.Context(), entry); err != nil {
		s.logger.Error("failed to create folder", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with folder metadata
	jsonSuccess(w, map[string]interface{}{
		"tag":          "folder",
		"id":           folderID,
		"name":         finalName,
		"path_display": "/" + finalName,
	})
}

// handleListFolderV3 handles POST /api/file/3/files/list_folder_v3.
// Lists files and folders in a directory.
// Expects JSON body: {"equipmentNo": "...", "id": 0, "recursive": false}
// Returns: {"cd": "000", "entries": [...]}
func (s *Server) handleListFolderV3(w http.ResponseWriter, r *http.Request) {
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

	directoryID := bodyInt(body, "id")
	recursive := bodyBool(body, "recursive")

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// List folder contents
	var entries []syncdb.FileEntry
	if recursive {
		entries, err = s.store.ListFolderRecursive(r.Context(), userID, directoryID)
	} else {
		entries, err = s.store.ListFolder(r.Context(), userID, directoryID)
	}

	if err != nil {
		s.logger.Error("failed to list folder", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Filter out stale entries (verify file exists on disk)
	var result []map[string]interface{}
	for _, entry := range entries {
		// Skip folders and check disk existence for files
		if !entry.IsFolder && !s.blobStore.Exists(r.Context(), entry.StorageKey) {
			continue
		}

		result = append(result, map[string]interface{}{
			"tag":               "file",
			"id":                entry.ID,
			"name":              entry.FileName,
			"path_display":      "/" + entry.FileName,
			"content_hash":      entry.MD5,
			"size":              entry.Size,
			"lastUpdateTime":    entry.UpdatedAt.Unix() * 1000, // Convert to milliseconds
			"is_downloadable":   !entry.IsFolder,
			"parent_path":       "/",
		})
	}

	// Return success with entries
	jsonSuccess(w, map[string]interface{}{
		"entries": result,
	})
}

// handleListFolderV2 handles POST /api/file/2/files/list_folder.
// Path-based version for backward compatibility.
// Expects JSON body: {"equipmentNo": "...", "path": "/"}
// Returns: {"cd": "000", "entries": [...]}
func (s *Server) handleListFolderV2(w http.ResponseWriter, r *http.Request) {
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

	folderPath := bodyStr(body, "path")
	if folderPath == "" {
		folderPath = "/"
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// For now, resolve root directory (id=0)
	// In a real implementation, we'd walk the path
	directoryID := int64(0)

	// List folder contents
	entries, err := s.store.ListFolder(r.Context(), userID, directoryID)
	if err != nil {
		s.logger.Error("failed to list folder", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Filter out stale entries
	var result []map[string]interface{}
	for _, entry := range entries {
		if !entry.IsFolder && !s.blobStore.Exists(r.Context(), entry.StorageKey) {
			continue
		}

		result = append(result, map[string]interface{}{
			"tag":               "file",
			"id":                entry.ID,
			"name":              entry.FileName,
			"path_display":      "/" + entry.FileName,
			"content_hash":      entry.MD5,
			"size":              entry.Size,
			"lastUpdateTime":    entry.UpdatedAt.Unix() * 1000,
			"is_downloadable":   !entry.IsFolder,
			"parent_path":       folderPath,
		})
	}

	// Return success with entries
	jsonSuccess(w, map[string]interface{}{
		"entries": result,
	})
}
