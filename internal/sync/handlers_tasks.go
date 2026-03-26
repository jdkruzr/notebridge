package sync

import (
	"errors"
	"net/http"
	"strings"

	"github.com/sysop/notebridge/internal/syncdb"
)

// camelToSnake converts camelCase to snake_case
func camelToSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteRune('_')
			result.WriteRune(r + 32) // convert to lowercase
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// convertFieldNames converts API field names (camelCase) to database field names (snake_case)
func convertFieldNames(fields map[string]interface{}) map[string]interface{} {
	converted := make(map[string]interface{})
	for k, v := range fields {
		converted[camelToSnake(k)] = v
	}
	return converted
}

// handleCreateScheduleGroup handles POST /api/file/schedule/group.
// Creates a schedule group (task list).
// Expects JSON body: {"taskListId", "title", "lastModified", "createTime"}
// Returns: {"cd": "000", "taskListId": "..."}
func (s *Server) handleCreateScheduleGroup(w http.ResponseWriter, r *http.Request) {
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
	taskListID := bodyStr(body, "taskListId")
	title := bodyStr(body, "title")
	if title == "" {
		jsonError(w, ErrBadRequest("missing or empty 'title' field"))
		return
	}

	lastModified := bodyInt(body, "lastModified")
	createTime := bodyInt(body, "createTime")

	// Create schedule group
	group := &syncdb.ScheduleGroup{
		TaskListID:   taskListID,
		UserID:       userID,
		Title:        title,
		LastModified: lastModified,
		CreateTime:   createTime,
	}

	if err := s.store.UpsertScheduleGroup(r.Context(), group); err != nil {
		s.logger.Error("failed to upsert schedule group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with taskListId
	jsonSuccess(w, map[string]interface{}{
		"taskListId": group.TaskListID,
	})
}

// handleUpdateScheduleGroup handles PUT /api/file/schedule/group.
// Updates a schedule group (task list).
// Expects JSON body: {"taskListId", "title", "lastModified"}
// Returns: {"cd": "000"}
func (s *Server) handleUpdateScheduleGroup(w http.ResponseWriter, r *http.Request) {
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
	taskListID := bodyStr(body, "taskListId")
	if taskListID == "" {
		jsonError(w, ErrBadRequest("missing or empty 'taskListId' field"))
		return
	}

	// Build partial update map
	updates := make(map[string]interface{})
	if title := bodyStr(body, "title"); title != "" {
		updates["title"] = title
	}
	if lastModified := bodyInt(body, "lastModified"); lastModified != 0 {
		updates["last_modified"] = lastModified
	}

	if len(updates) == 0 {
		jsonError(w, ErrBadRequest("no update fields provided"))
		return
	}

	// Update schedule group
	if err := s.store.UpdateScheduleGroup(r.Context(), taskListID, userID, updates); err != nil {
		if errors.Is(err, syncdb.ErrTaskGroupNotFound) {
			jsonError(w, ErrTaskGroupNotFound())
			return
		}
		s.logger.Error("failed to update schedule group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleDeleteScheduleGroup handles DELETE /api/file/schedule/group/{taskListId}.
// Deletes a schedule group and all its tasks.
// Returns: {"cd": "000"}
func (s *Server) handleDeleteScheduleGroup(w http.ResponseWriter, r *http.Request) {
	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract taskListId from URL path
	taskListID := r.PathValue("taskListId")
	if taskListID == "" {
		jsonError(w, ErrBadRequest("missing 'taskListId' in path"))
		return
	}

	// Delete schedule group (cascading delete of tasks)
	if err := s.store.DeleteScheduleGroup(r.Context(), taskListID, userID); err != nil {
		s.logger.Error("failed to delete schedule group", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleListScheduleGroups handles POST /api/file/schedule/group/all.
// Lists schedule groups for the user.
// Expects JSON body: {"maxResults": 20, "pageToken": 1}
// Returns: {"cd": "000", "scheduleTaskGroup": [...], "pageToken": 2}
func (s *Server) handleListScheduleGroups(w http.ResponseWriter, r *http.Request) {
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
	maxResults := bodyInt(body, "maxResults")
	if maxResults <= 0 {
		maxResults = 20
	}
	pageToken := bodyInt(body, "pageToken")
	if pageToken <= 0 {
		pageToken = 1
	}

	// List schedule groups
	groups, total, err := s.store.ListScheduleGroups(r.Context(), userID, int(pageToken), int(maxResults))
	if err != nil {
		s.logger.Error("failed to list schedule groups", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Build response
	response := map[string]interface{}{
		"scheduleTaskGroup": groups,
	}

	// Add next page token if there are more results
	totalPages := (total + int(maxResults) - 1) / int(maxResults)
	if pageToken < int64(totalPages) {
		response["pageToken"] = pageToken + 1
	}

	// Return success
	jsonSuccess(w, response)
}

// handleCreateScheduleTask handles POST /api/file/schedule/task.
// Creates a schedule task in a task list.
// Expects JSON body: all task fields
// Returns: {"cd": "000", "taskId": "..."}
func (s *Server) handleCreateScheduleTask(w http.ResponseWriter, r *http.Request) {
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
	task := &syncdb.ScheduleTask{
		TaskID:            bodyStr(body, "taskId"),
		UserID:            userID,
		TaskListID:        bodyStr(body, "taskListId"),
		Title:             bodyStr(body, "title"),
		Detail:            bodyStr(body, "detail"),
		Status:            bodyStr(body, "status"),
		Importance:        bodyStr(body, "importance"),
		Recurrence:        bodyStr(body, "recurrence"),
		Links:             bodyStr(body, "links"),
		IsReminderOn:      bodyStr(body, "isReminderOn"),
		DueTime:           bodyInt(body, "dueTime"),
		CompletedTime:     bodyInt(body, "completedTime"),
		LastModified:      bodyInt(body, "lastModified"),
		Sort:              bodyInt(body, "sort"),
		SortCompleted:     bodyInt(body, "sortCompleted"),
		PlanerSort:        bodyInt(body, "planerSort"),
		SortTime:          bodyInt(body, "sortTime"),
		PlanerSortTime:    bodyInt(body, "planerSortTime"),
		AllSort:           bodyInt(body, "allSort"),
		AllSortCompleted:  bodyInt(body, "allSortCompleted"),
		AllSortTime:       bodyInt(body, "allSortTime"),
		RecurrenceID:      bodyStr(body, "recurrenceId"),
	}

	// Upsert schedule task
	if err := s.store.UpsertScheduleTask(r.Context(), task); err != nil {
		if errors.Is(err, syncdb.ErrTaskGroupNotFound) {
			jsonError(w, ErrTaskGroupNotFound())
			return
		}
		s.logger.Error("failed to upsert schedule task", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success with taskId
	jsonSuccess(w, map[string]interface{}{
		"taskId": task.TaskID,
	})
}

// handleBatchUpdateTasks handles PUT /api/file/schedule/task/list.
// Batch updates multiple tasks.
// Expects JSON body: {"updateScheduleTaskList": [{"taskId": "...", "fields": {...}}, ...]}
// Returns: {"cd": "000"}
func (s *Server) handleBatchUpdateTasks(w http.ResponseWriter, r *http.Request) {
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

	// Extract updateScheduleTaskList array
	updateList, ok := body["updateScheduleTaskList"].([]interface{})
	if !ok {
		jsonError(w, ErrBadRequest("missing or invalid 'updateScheduleTaskList' field"))
		return
	}

	// Parse task updates
	var updates []syncdb.TaskUpdate
	for _, item := range updateList {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			jsonError(w, ErrBadRequest("invalid task update item"))
			return
		}

		taskID := bodyStr(itemMap, "taskId")
		if taskID == "" {
			jsonError(w, ErrBadRequest("missing 'taskId' in update item"))
			return
		}

		// Support both formats:
		// - Nested: {"taskId": "...", "fields": {"status": "1"}} (Settings app)
		// - Flat:   {"taskId": "...", "status": "1", ...} (file sync client)
		var fieldsRaw map[string]interface{}
		if nested, ok := itemMap["fields"].(map[string]interface{}); ok {
			fieldsRaw = nested
		} else {
			fieldsRaw = itemMap
		}
		fields := convertFieldNames(fieldsRaw)
		delete(fields, "task_id")

		updates = append(updates, syncdb.TaskUpdate{
			TaskID: taskID,
			Fields: fields,
		})
	}

	// Batch update tasks
	if err := s.store.BatchUpdateTasks(r.Context(), userID, updates); err != nil {
		if errors.Is(err, syncdb.ErrTaskNotFound) {
			jsonError(w, ErrTaskNotFound())
			return
		}
		s.logger.Error("failed to batch update tasks", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleDeleteScheduleTask handles DELETE /api/file/schedule/task/{taskId}.
// Deletes a schedule task.
// Returns: {"cd": "000"}
func (s *Server) handleDeleteScheduleTask(w http.ResponseWriter, r *http.Request) {
	// Get userID from context
	userID := UserIDFromContext(r.Context())
	if userID == 0 {
		jsonError(w, ErrInvalidToken())
		return
	}

	// Extract taskId from URL path
	taskID := r.PathValue("taskId")
	if taskID == "" {
		jsonError(w, ErrBadRequest("missing 'taskId' in path"))
		return
	}

	// Delete schedule task
	if err := s.store.DeleteScheduleTask(r.Context(), taskID, userID); err != nil {
		s.logger.Error("failed to delete schedule task", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Return success
	jsonSuccess(w, nil)
}

// handleListScheduleTasks handles POST /api/file/schedule/task/all.
// Lists schedule tasks for the user with optional sync token filtering.
// Expects JSON body: {"maxResults": 20, "nextPageTokens": 1, "nextSyncToken": timestamp}
// Returns: {"cd": "000", "scheduleTask": [...], "nextPageToken": 2, "nextSyncToken": timestamp}
func (s *Server) handleListScheduleTasks(w http.ResponseWriter, r *http.Request) {
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
	maxResults := bodyInt(body, "maxResults")
	if maxResults <= 0 {
		maxResults = 20
	}
	pageToken := bodyInt(body, "nextPageTokens")
	if pageToken <= 0 {
		pageToken = 1
	}

	// Extract optional sync token
	var syncToken *int64
	if rawSyncToken := bodyInt(body, "nextSyncToken"); rawSyncToken > 0 {
		syncToken = &rawSyncToken
	}

	// List schedule tasks
	tasks, totalCount, nextSyncToken, err := s.store.ListScheduleTasks(r.Context(), userID, int(pageToken), int(maxResults), syncToken)
	if err != nil {
		s.logger.Error("failed to list schedule tasks", "error", err)
		jsonError(w, ErrInternal("internal server error"))
		return
	}

	// Build response
	response := map[string]interface{}{
		"scheduleTask": tasks,
	}

	// Determine if there are more results using total count
	totalPages := (totalCount + int(maxResults) - 1) / int(maxResults)
	if pageToken < int64(totalPages) {
		response["nextPageToken"] = pageToken + 1
	}

	// Add next sync token if returned
	if nextSyncToken != nil {
		response["nextSyncToken"] = *nextSyncToken
	}

	// Return success
	jsonSuccess(w, response)
}
