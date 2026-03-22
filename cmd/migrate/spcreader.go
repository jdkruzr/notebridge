package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// SPCUser represents a user in SPC's MariaDB.
type SPCUser struct {
	UserID       int64
	Email        string
	PasswordHash string // MD5 hex
	Username     string
}

// SPCFile represents a file or folder in SPC's MariaDB.
type SPCFile struct {
	ID          int64
	DirectoryID int64
	FileName    string
	InnerName   string
	MD5         string
	Size        int64
	IsFolder    bool
	CreateTime  int64
	UpdateTime  int64
}

// SPCTask represents a task in SPC's MariaDB.
type SPCTask struct {
	TaskID       string
	TaskListID   string
	Title        string
	Detail       string
	Status       string
	Importance   string
	DueTime      int64
	CompletedTime int64
	LastModified int64
	Recurrence   string
	IsReminderOn string
	Links        string
}

// SPCTaskGroup represents a task group/list in SPC's MariaDB.
type SPCTaskGroup struct {
	TaskListID   string
	Title        string
	LastModified int64
}

// SPCSummary represents a summary/digest in SPC's MariaDB.
type SPCSummary struct {
	ID                      int64
	UniqueIdentifier        string
	Name                    string
	Description             string
	FileID                  int64
	ParentUniqueIdentifier  string
	Content                 string
	DataSource              string
	SourcePath              string
	SourceType              string
	Tags                    string
	MD5Hash                 string
	Metadata                string
	IsSummaryGroup          bool
	Author                  string
	CreationTime            int64
	LastModifiedTime        int64
	HandwriteInnerName      string
}

// SPCReader wraps a MariaDB connection for reading SPC data.
type SPCReader struct {
	db *sql.DB
}

// NewSPCReader connects to SPC's MariaDB and returns a reader.
func NewSPCReader(dsn string) (*SPCReader, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open mysql connection: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping mysql: %w", err)
	}

	return &SPCReader{db: db}, nil
}

// Close closes the database connection.
func (r *SPCReader) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

// ReadUser reads the first normal user from SPC.
func (r *SPCReader) ReadUser(ctx context.Context) (*SPCUser, error) {
	query := `SELECT user_id, email, password, user_name FROM u_user WHERE is_normal = 'Y' LIMIT 1`

	var user SPCUser
	err := r.db.QueryRowContext(ctx, query).Scan(&user.UserID, &user.Email, &user.PasswordHash, &user.Username)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no normal user found in SPC")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read user: %w", err)
	}

	return &user, nil
}

// ReadFiles reads all active files for a user, ordered by folder first.
func (r *SPCReader) ReadFiles(ctx context.Context, userID int64) ([]SPCFile, error) {
	query := `
		SELECT id, directory_id, file_name, inner_name, md5, size, is_folder, create_time, update_time
		FROM f_user_file
		WHERE user_id = ? AND is_active = 'Y'
		ORDER BY is_folder DESC, directory_id, file_name
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query files: %w", err)
	}
	defer rows.Close()

	var files []SPCFile
	for rows.Next() {
		var f SPCFile
		var isFolderStr string
		err := rows.Scan(&f.ID, &f.DirectoryID, &f.FileName, &f.InnerName, &f.MD5, &f.Size, &isFolderStr, &f.CreateTime, &f.UpdateTime)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file row: %w", err)
		}
		f.IsFolder = isFolderStr == "Y"
		files = append(files, f)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating files: %w", err)
	}

	return files, nil
}

// ReadTasks reads all non-deleted tasks for a user.
func (r *SPCReader) ReadTasks(ctx context.Context, userID int64) ([]SPCTask, error) {
	query := `
		SELECT task_id, task_list_id, title, detail, status, importance, due_time, completed_time, last_modified, recurrence, is_reminder_on, links
		FROM t_schedule_task
		WHERE user_id = ? AND is_deleted = 'N'
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []SPCTask
	for rows.Next() {
		var t SPCTask
		err := rows.Scan(&t.TaskID, &t.TaskListID, &t.Title, &t.Detail, &t.Status, &t.Importance,
			&t.DueTime, &t.CompletedTime, &t.LastModified, &t.Recurrence, &t.IsReminderOn, &t.Links)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task row: %w", err)
		}
		tasks = append(tasks, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tasks: %w", err)
	}

	return tasks, nil
}

// ReadTaskGroups reads all task groups for a user.
func (r *SPCReader) ReadTaskGroups(ctx context.Context, userID int64) ([]SPCTaskGroup, error) {
	query := `SELECT task_list_id, title, last_modified FROM t_schedule_task_group WHERE user_id = ?`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		// Table might not exist; try inferring from tasks
		return r.inferTaskGroups(ctx, userID)
	}
	defer rows.Close()

	var groups []SPCTaskGroup
	for rows.Next() {
		var g SPCTaskGroup
		err := rows.Scan(&g.TaskListID, &g.Title, &g.LastModified)
		if err != nil {
			return nil, fmt.Errorf("failed to scan task group row: %w", err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating task groups: %w", err)
	}

	return groups, nil
}

// inferTaskGroups infers task groups from distinct task_list_id values in tasks.
func (r *SPCReader) inferTaskGroups(ctx context.Context, userID int64) ([]SPCTaskGroup, error) {
	query := `
		SELECT DISTINCT task_list_id, title, MAX(last_modified) as last_modified
		FROM t_schedule_task
		WHERE user_id = ? AND is_deleted = 'N'
		GROUP BY task_list_id, title
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to infer task groups: %w", err)
	}
	defer rows.Close()

	var groups []SPCTaskGroup
	for rows.Next() {
		var g SPCTaskGroup
		err := rows.Scan(&g.TaskListID, &g.Title, &g.LastModified)
		if err != nil {
			return nil, fmt.Errorf("failed to scan inferred task group row: %w", err)
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating inferred task groups: %w", err)
	}

	return groups, nil
}

// ReadSummaries reads all non-deleted summaries for a user.
func (r *SPCReader) ReadSummaries(ctx context.Context, userID int64) ([]SPCSummary, error) {
	query := `
		SELECT id, unique_identifier, name, description, file_id, parent_unique_identifier, content, data_source, source_path, source_type, tags, md5_hash, metadata, is_summary_group, author, creation_time, last_modified_time, handwrite_inner_name
		FROM t_summary
		WHERE user_id = ? AND (is_deleted IS NULL OR is_deleted != 'Y')
	`

	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query summaries: %w", err)
	}
	defer rows.Close()

	var summaries []SPCSummary
	for rows.Next() {
		var s SPCSummary
		var isSummaryGroupStr string
		err := rows.Scan(&s.ID, &s.UniqueIdentifier, &s.Name, &s.Description, &s.FileID, &s.ParentUniqueIdentifier,
			&s.Content, &s.DataSource, &s.SourcePath, &s.SourceType, &s.Tags, &s.MD5Hash, &s.Metadata,
			&isSummaryGroupStr, &s.Author, &s.CreationTime, &s.LastModifiedTime, &s.HandwriteInnerName)
		if err != nil {
			return nil, fmt.Errorf("failed to scan summary row: %w", err)
		}
		s.IsSummaryGroup = isSummaryGroupStr == "Y"
		summaries = append(summaries, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating summaries: %w", err)
	}

	return summaries, nil
}
