package sync

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strings"

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
		var err error
		parentID, err = s.resolvePathToDirectoryID(r.Context(), userID, parentPath)
		if err != nil {
			s.logger.Error("failed to resolve parent path", "path", parentPath, "error", err)
			jsonError(w, ErrBadRequest("failed to resolve parent folder path"))
			return
		}
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
		"path_display": finalName,
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

	// Build the base path for this directory ID
	basePath := s.buildPathForDirectoryID(r.Context(), userID, directoryID)

	result := s.listEntriesWithPaths(r.Context(), userID, directoryID, basePath, recursive)

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

	folderPath := strings.Trim(bodyStr(body, "path"), "/")
	recursive := bodyBool(body, "recursive")

	s.logger.Info("list_folder_v2", "path", folderPath, "recursive", recursive, "equipmentNo", equipmentNo)

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Resolve path to directory ID
	directoryID := int64(0)
	if folderPath != "" {
		var resolveErr error
		directoryID, resolveErr = s.resolvePathToDirectoryID(r.Context(), userID, folderPath)
		if resolveErr != nil {
			// Path doesn't exist — return empty entries (not an error)
			jsonSuccess(w, map[string]interface{}{"equipmentNo": equipmentNo, "entries": []interface{}{}})
			return
		}
	}

	result := s.listEntriesWithPaths(r.Context(), userID, directoryID, folderPath, recursive)

	// Return success with entries
	jsonSuccess(w, map[string]interface{}{
		"entries": result,
	})
}

// listEntriesWithPaths lists folder contents with correct path_display values.
// Matches opennotecloud's recursive listing approach: path is built by accumulating
// directory names as we descend, so each entry gets its full path.
func (s *Server) listEntriesWithPaths(ctx context.Context, userID, dirID int64, basePath string, recursive bool) []map[string]interface{} {
	entries, err := s.store.ListFolder(ctx, userID, dirID)
	if err != nil {
		s.logger.Error("failed to list folder", "error", err)
		return []map[string]interface{}{}
	}

	var result []map[string]interface{}
	for _, entry := range entries {
		// Skip files that don't exist on disk (folders are always listed)
		if !entry.IsFolder && entry.StorageKey != "" && !s.blobStore.Exists(ctx, entry.StorageKey) {
			continue
		}

		tag := "file"
		if entry.IsFolder {
			tag = "folder"
		}

		// Build path_display: basePath + "/" + name (or just name if basePath is empty)
		pathDisplay := entry.FileName
		if basePath != "" {
			pathDisplay = basePath + "/" + entry.FileName
		}

		// SPC uses null lastUpdateTime for folders
		var lastUpdateTime interface{}
		if !entry.IsFolder {
			lastUpdateTime = entry.UpdatedAt.Unix() * 1000
		}

		result = append(result, map[string]interface{}{
			"tag":            tag,
			"id":             entry.ID,
			"name":           entry.FileName,
			"path_display":   pathDisplay,
			"content_hash":   entry.MD5,
			"size":           entry.Size,
			"lastUpdateTime": lastUpdateTime,
			"_downloadable":  !entry.IsFolder,
			"parent_path":    basePath,
		})

		// Recurse into subfolders
		if recursive && entry.IsFolder {
			result = append(result, s.listEntriesWithPaths(ctx, userID, entry.ID, pathDisplay, true)...)
		}
	}

	if result == nil {
		result = []map[string]interface{}{}
	}
	return result
}

// buildPathForDirectoryID reconstructs the full path for a directory ID
// by walking up the parent chain. Returns "" for root (id=0).
func (s *Server) buildPathForDirectoryID(ctx context.Context, userID, dirID int64) string {
	if dirID == 0 {
		return ""
	}
	var parts []string
	currentID := dirID
	for currentID != 0 {
		file, err := s.store.GetFile(ctx, currentID, userID)
		if err != nil || file == nil {
			break
		}
		parts = append([]string{file.FileName}, parts...)
		currentID = file.DirectoryID
	}
	return strings.Join(parts, "/")
}

// resolvePathToDirectoryID walks a path and returns the directory ID.
// Path components are split by "/" and each is looked up as a child folder.
// Creates intermediate folders if they don't exist (for collection sync).
func (s *Server) resolvePathToDirectoryID(ctx context.Context, userID int64, folderPath string) (int64, error) {
	if folderPath == "/" || folderPath == "" || folderPath == "." {
		return 0, nil // Root directory
	}

	// Split path into components, filtering empty parts
	components := strings.Split(folderPath, "/")
	var parts []string
	for _, comp := range components {
		if comp != "" && comp != "." {
			parts = append(parts, comp)
		}
	}

	// Walk the path, creating folders as needed
	currentID := int64(0) // Start at root
	for _, folderName := range parts {
		// Check if folder exists at current level
		existing, err := s.store.GetFileByPath(ctx, userID, currentID, folderName)
		if err != nil {
			return 0, err
		}

		if existing != nil {
			if !existing.IsFolder {
				return 0, errors.New("path component is not a folder")
			}
			currentID = existing.ID
		} else {
			// Create the folder
			newID := s.snowflake.Generate()
			folderEntry := &syncdb.FileEntry{
				ID:          newID,
				UserID:      userID,
				DirectoryID: currentID,
				FileName:    folderName,
				InnerName:   folderName,
				StorageKey:  "",
				MD5:         "",
				Size:        0,
				IsFolder:    true,
				IsActive:    true,
			}
			if err := s.store.CreateFile(ctx, folderEntry); err != nil {
				return 0, err
			}
			currentID = newID
		}
	}

	return currentID, nil
}
