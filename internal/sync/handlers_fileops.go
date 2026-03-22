package sync

import (
	"context"
	"net/http"
	"path"
	"strconv"

	"github.com/sysop/notebridge/internal/syncdb"
)

// handleDeleteV3 handles POST /api/file/3/files/delete_folder_v3.
// Soft deletes a file or folder.
// Expects JSON body: {"equipmentNo": "...", "id": 12345}
// Returns: {"cd": "000", "id": 12345}
func (s *Server) handleDeleteV3(w http.ResponseWriter, r *http.Request) {
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

	if file == nil {
		jsonError(w, ErrBadRequest("file not found"))
		return
	}

	// Collect all IDs to delete (including children if folder)
	var idsToDelete []int64
	if file.IsFolder {
		// Recursively collect all child IDs
		entries, err := s.store.ListFolderRecursive(r.Context(), userID, fileID)
		if err != nil {
			s.logger.Error("failed to list folder recursively", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}

		// Add all child IDs
		for _, entry := range entries {
			idsToDelete = append(idsToDelete, entry.ID)
		}
	}

	// Always add the file itself
	idsToDelete = append(idsToDelete, fileID)

	// Soft delete each file and delete from disk
	for _, id := range idsToDelete {
		// Soft delete from DB
		if err := s.store.SoftDelete(r.Context(), id, userID); err != nil {
			s.logger.Error("failed to soft delete file", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
		// Note: Blob deletion is implicit - we mark as deleted in DB but don't physically delete
		// This allows recovery from the recycle_files table if needed
	}

	// Refresh sync lock
	if err := s.store.RefreshLock(r.Context(), userID); err != nil {
		// Not fatal, continue
	}

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"id": fileID,
	})
}

// handleQueryV3 handles POST /api/file/3/files/query_v3.
// Gets metadata for a file or folder.
// Expects JSON body: {"equipmentNo": "...", "id": 12345}
// Returns: {"cd": "000", "entries": [{...}]}
func (s *Server) handleQueryV3(w http.ResponseWriter, r *http.Request) {
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

	if file == nil {
		jsonError(w, ErrBadRequest("file not found"))
		return
	}

	// Format as entriesVO
	entry := map[string]interface{}{
		"tag":             "file",
		"id":              file.ID,
		"name":            file.FileName,
		"path_display":    "/" + file.FileName,
		"content_hash":    file.MD5,
		"size":            file.Size,
		"lastUpdateTime":  file.UpdatedAt.Unix() * 1000,
		"is_downloadable": !file.IsFolder,
	}

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"entries": []map[string]interface{}{entry},
	})
}

// handleQueryByPathV3 handles POST /api/file/3/files/query/by/path_v3.
// Gets metadata for a file or folder by path.
// Expects JSON body: {"equipmentNo": "...", "path": "/file.txt"}
// Returns: {"cd": "000", "entries": [{...}]}
func (s *Server) handleQueryByPathV3(w http.ResponseWriter, r *http.Request) {
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
	if filePath == "" {
		jsonError(w, ErrBadRequest("missing or empty 'path' field"))
		return
	}

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Parse path to get file name
	fileName := path.Base(filePath)

	// Get file entry from root (simplified - assume root directory id=0)
	file, err := s.store.GetFileByPath(r.Context(), userID, 0, fileName)
	if err != nil {
		s.logger.Error("failed to get file by path", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if file == nil {
		jsonError(w, ErrBadRequest("file not found"))
		return
	}

	// Format as entriesVO
	entry := map[string]interface{}{
		"tag":             "file",
		"id":              file.ID,
		"name":            file.FileName,
		"path_display":    "/" + file.FileName,
		"content_hash":    file.MD5,
		"size":            file.Size,
		"lastUpdateTime":  file.UpdatedAt.Unix() * 1000,
		"is_downloadable": !file.IsFolder,
	}

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"entries": []map[string]interface{}{entry},
	})
}

// handleMoveV3 handles POST /api/file/3/files/move_v3.
// Moves or renames a file.
// Expects JSON body: {"equipmentNo": "...", "id": 12345, "to_path": "/folder/", "autorename": true|false}
// Returns: {"cd": "000", "entries": [{...}]}
func (s *Server) handleMoveV3(w http.ResponseWriter, r *http.Request) {
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

	toPath := bodyStr(body, "to_path")
	if toPath == "" {
		jsonError(w, ErrBadRequest("missing or empty 'to_path' field"))
		return
	}

	autorename := bodyBool(body, "autorename")

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Get source file
	srcFile, err := s.store.GetFile(r.Context(), fileID, userID)
	if err != nil {
		s.logger.Error("failed to get source file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if srcFile == nil {
		jsonError(w, ErrBadRequest("source file not found"))
		return
	}

	// Parse destination path
	newFileName := path.Base(toPath)
	newDirPath := path.Dir(toPath)

	// Resolve destination directory ID
	newDirectoryID := int64(0)
	if newDirPath != "/" && newDirPath != "." {
		var err error
		newDirectoryID, err = s.resolvePathToDirectoryID(r.Context(), userID, newDirPath)
		if err != nil {
			s.logger.Error("failed to resolve destination path", "path", newDirPath, "error", err)
			jsonError(w, ErrBadRequest("failed to resolve destination folder path"))
			return
		}
	}

	// Circular move detection for folders
	if srcFile.IsFolder {
		ancestors, err := s.store.GetAncestorIDs(r.Context(), newDirectoryID, 100)
		if err != nil {
			s.logger.Error("failed to get ancestor IDs", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}

		if IsCircularMove(fileID, ancestors) {
			jsonError(w, ErrCircularMove())
			return
		}
	}

	// Check for name collision at destination
	existing, err := s.store.GetFileByPath(r.Context(), userID, newDirectoryID, newFileName)
	if err != nil {
		s.logger.Error("failed to check destination", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	var finalName string
	if existing != nil && existing.ID != fileID {
		if !autorename {
			jsonError(w, ErrNameCollision())
			return
		}
		// Autorename
		existingNames, err := s.store.FindByName(r.Context(), userID, newDirectoryID, newFileName)
		if err != nil {
			s.logger.Error("failed to find existing names", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
		finalName = AutoRename(newFileName, existingNames)
	} else {
		finalName = newFileName
	}

	// Update file in DB
	if err := s.store.MoveFile(r.Context(), fileID, newDirectoryID, finalName); err != nil {
		s.logger.Error("failed to move file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Refresh sync lock
	if err := s.store.RefreshLock(r.Context(), userID); err != nil {
		// Not fatal
	}

	// Get updated file and return
	updatedFile, _ := s.store.GetFile(r.Context(), fileID, userID)
	if updatedFile == nil {
		updatedFile = srcFile // Fallback
	}

	entry := map[string]interface{}{
		"tag":             "file",
		"id":              updatedFile.ID,
		"name":            updatedFile.FileName,
		"path_display":    "/" + updatedFile.FileName,
		"content_hash":    updatedFile.MD5,
		"size":            updatedFile.Size,
		"lastUpdateTime":  updatedFile.UpdatedAt.Unix() * 1000,
		"is_downloadable": !updatedFile.IsFolder,
	}

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"entries": []map[string]interface{}{entry},
	})
}

// handleCopyV3 handles POST /api/file/3/files/copy_v3.
// Copies a file or folder.
// Expects JSON body: {"equipmentNo": "...", "id": 12345, "to_path": "/folder/", "autorename": true|false}
// Returns: {"cd": "000", "entries": [{...}]}
func (s *Server) handleCopyV3(w http.ResponseWriter, r *http.Request) {
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

	toPath := bodyStr(body, "to_path")
	if toPath == "" {
		jsonError(w, ErrBadRequest("missing or empty 'to_path' field"))
		return
	}

	autorename := bodyBool(body, "autorename")

	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Get source file
	srcFile, err := s.store.GetFile(r.Context(), fileID, userID)
	if err != nil {
		s.logger.Error("failed to get source file", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	if srcFile == nil {
		jsonError(w, ErrBadRequest("source file not found"))
		return
	}

	// Parse destination path
	newFileName := path.Base(toPath)
	newDirPath := path.Dir(toPath)

	// Resolve destination directory ID
	newDirectoryID := int64(0)
	if newDirPath != "/" && newDirPath != "." {
		var err error
		newDirectoryID, err = s.resolvePathToDirectoryID(r.Context(), userID, newDirPath)
		if err != nil {
			s.logger.Error("failed to resolve destination path", "path", newDirPath, "error", err)
			jsonError(w, ErrBadRequest("failed to resolve destination folder path"))
			return
		}
	}

	// Check for name collision
	existing, err := s.store.GetFileByPath(r.Context(), userID, newDirectoryID, newFileName)
	if err != nil {
		s.logger.Error("failed to check destination", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	var finalName string
	if existing != nil {
		if !autorename {
			jsonError(w, ErrNameCollision())
			return
		}
		// Autorename
		existingNames, err := s.store.FindByName(r.Context(), userID, newDirectoryID, newFileName)
		if err != nil {
			s.logger.Error("failed to find existing names", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
		finalName = AutoRename(newFileName, existingNames)
	} else {
		finalName = newFileName
	}

	// Create new Snowflake ID for the copy
	newFileID := s.snowflake.Generate()

	// For files, copy blob data
	var newStorageKey string
	if !srcFile.IsFolder {
		newStorageKey = srcFile.StorageKey + ".copy." + strconv.FormatInt(newFileID, 10)

		// Copy file on disk (read from blob, write to new key)
		srcReader, _, err := s.blobStore.Get(r.Context(), srcFile.StorageKey)
		if err != nil {
			s.logger.Error("failed to read source file", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
		defer srcReader.Close()

		// Write to new key
		_, md5hex, err := s.blobStore.Put(r.Context(), newStorageKey, srcReader)
		if err != nil {
			s.logger.Error("failed to copy file to blob", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}

		// Create new file entry in DB with new ID
		copyEntry := &syncdb.FileEntry{
			ID:          newFileID,
			UserID:      userID,
			DirectoryID: newDirectoryID,
			FileName:    finalName,
			InnerName:   finalName,
			StorageKey:  newStorageKey,
			MD5:         md5hex,
			Size:        srcFile.Size,
			IsFolder:    false,
			IsActive:    true,
		}

		if err := s.store.CreateFile(r.Context(), copyEntry); err != nil {
			s.logger.Error("failed to create copy entry", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
	} else {
		// For folders, create an empty folder entry
		copyEntry := &syncdb.FileEntry{
			ID:          newFileID,
			UserID:      userID,
			DirectoryID: newDirectoryID,
			FileName:    finalName,
			InnerName:   finalName,
			StorageKey:  "",
			MD5:         "",
			Size:        0,
			IsFolder:    true,
			IsActive:    true,
		}

		if err := s.store.CreateFile(r.Context(), copyEntry); err != nil {
			s.logger.Error("failed to create folder copy entry", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}

		// Recursively copy all children
		if err := s.recursiveFolderCopy(r.Context(), userID, srcFile.ID, newFileID); err != nil {
			s.logger.Error("failed to recursively copy folder children", "error", err)
			jsonError(w, ErrInternal("internal server error"))
			return
		}
	}

	// Refresh sync lock
	if err := s.store.RefreshLock(r.Context(), userID); err != nil {
		// Not fatal
	}

	// Get the new entry and return
	newEntry, _ := s.store.GetFile(r.Context(), newFileID, userID)
	if newEntry == nil {
		newEntry = srcFile // Fallback
	}

	entry := map[string]interface{}{
		"tag":             "file",
		"id":              newEntry.ID,
		"name":            newEntry.FileName,
		"path_display":    "/" + newEntry.FileName,
		"content_hash":    newEntry.MD5,
		"size":            newEntry.Size,
		"lastUpdateTime":  newEntry.UpdatedAt.Unix() * 1000,
		"is_downloadable": !newEntry.IsFolder,
	}

	// Return success
	jsonSuccess(w, map[string]interface{}{
		"entries": []map[string]interface{}{entry},
	})
}

// handleSpaceUsage handles POST /api/file/3/files/space_usage.
// Gets the total space used by the user.
// Expects JSON body: {"equipmentNo": "..."}
// Returns: {"cd": "000", "used": 12345, "total": 1099511627776}
func (s *Server) handleSpaceUsage(w http.ResponseWriter, r *http.Request) {
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

	// Get space usage
	used, err := s.store.SpaceUsage(r.Context(), userID)
	if err != nil {
		s.logger.Error("failed to get space usage", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with space info
	// Assume 1TB quota (1099511627776 bytes)
	jsonSuccess(w, map[string]interface{}{
		"used":  used,
		"total": int64(1099511627776),
	})
}

// recursiveFolderCopy recursively copies all children of a source folder to a destination folder.
// For each child, it creates a new entry with a new Snowflake ID and copies blob content if needed.
func (s *Server) recursiveFolderCopy(ctx context.Context, userID int64, srcFolderID int64, dstFolderID int64) error {
	// List all children of source folder
	children, err := s.store.ListFolder(ctx, userID, srcFolderID)
	if err != nil {
		return err
	}

	for _, child := range children {
		// Generate new ID for the copy
		newID := s.snowflake.Generate()

		if child.IsFolder {
			// Create folder entry
			folderEntry := &syncdb.FileEntry{
				ID:          newID,
				UserID:      userID,
				DirectoryID: dstFolderID,
				FileName:    child.FileName,
				InnerName:   child.InnerName,
				StorageKey:  "",
				MD5:         "",
				Size:        0,
				IsFolder:    true,
				IsActive:    true,
			}
			if err := s.store.CreateFile(ctx, folderEntry); err != nil {
				return err
			}

			// Recursively copy children
			if err := s.recursiveFolderCopy(ctx, userID, child.ID, newID); err != nil {
				return err
			}
		} else {
			// For files, copy blob data and create entry
			newStorageKey := child.StorageKey + ".copy." + strconv.FormatInt(newID, 10)

			srcReader, _, err := s.blobStore.Get(ctx, child.StorageKey)
			if err != nil {
				return err
			}
			defer srcReader.Close()

			_, md5hex, err := s.blobStore.Put(ctx, newStorageKey, srcReader)
			if err != nil {
				return err
			}

			fileEntry := &syncdb.FileEntry{
				ID:          newID,
				UserID:      userID,
				DirectoryID: dstFolderID,
				FileName:    child.FileName,
				InnerName:   child.InnerName,
				StorageKey:  newStorageKey,
				MD5:         md5hex,
				Size:        child.Size,
				IsFolder:    false,
				IsActive:    true,
			}
			if err := s.store.CreateFile(ctx, fileEntry); err != nil {
				return err
			}
		}
	}

	return nil
}
