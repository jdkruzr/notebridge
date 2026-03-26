package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/sync"
	"github.com/sysop/notebridge/internal/syncdb"
)

// MigrationStats tracks migration progress and issues.
type MigrationStats struct {
	UsersMigrated      int
	FoldersMigrated    int
	FilesMigrated      int
	BytesMigrated      int64
	MD5Mismatches      int
	MissingFiles       int
	TaskGroupsMigrated int
	TasksMigrated      int
	SummariesMigrated  int
	HandwriteFilesCopied int
	HandwriteFilesMissing int
}

// SPCReaderInterface defines the interface for reading from SPC.
type SPCReaderInterface interface {
	ReadUser(ctx context.Context) (*SPCUser, error)
	ReadFiles(ctx context.Context, userID int64) ([]SPCFile, error)
	ReadTasks(ctx context.Context, userID int64) ([]SPCTask, error)
	ReadTaskGroups(ctx context.Context, userID int64) ([]SPCTaskGroup, error)
	ReadSummaries(ctx context.Context, userID int64) ([]SPCSummary, error)
	Close() error
}

// Migrator orchestrates the migration from SPC to NoteBridge.
type Migrator struct {
	spcReader  SPCReaderInterface
	syncStore  *syncdb.Store
	blobStore  *blob.LocalStore
	snowflake  *sync.SnowflakeGenerator
	spcPath    string
	logger     *slog.Logger
	dryRun     bool
	stats      MigrationStats
	dirMap     map[int64]int64 // SPC directory_id -> NoteBridge file ID mapping
}

// NewMigrator creates a new migrator.
func NewMigrator(spc SPCReaderInterface, store *syncdb.Store, blobst *blob.LocalStore, sf *sync.SnowflakeGenerator, spcPath string, logger *slog.Logger) *Migrator {
	return &Migrator{
		spcReader: spc,
		syncStore: store,
		blobStore: blobst,
		snowflake: sf,
		spcPath:   spcPath,
		logger:    logger,
		dirMap:    make(map[int64]int64),
	}
}

// SetDryRun sets whether this migration is a dry run (no writes).
func (m *Migrator) SetDryRun(dryRun bool) {
	m.dryRun = dryRun
}

// Run orchestrates the full migration.
func (m *Migrator) Run(ctx context.Context) error {
	m.logger.Info("Starting migration from SPC to NoteBridge")

	// Step 1: Migrate user
	if err := m.migrateUser(ctx); err != nil {
		return fmt.Errorf("failed to migrate user: %w", err)
	}

	// Step 2: Migrate files
	if err := m.migrateFiles(ctx); err != nil {
		return fmt.Errorf("failed to migrate files: %w", err)
	}

	// Step 3: Migrate task groups
	if err := m.migrateTaskGroups(ctx); err != nil {
		return fmt.Errorf("failed to migrate task groups: %w", err)
	}

	// Step 4: Migrate tasks
	if err := m.migrateTasks(ctx); err != nil {
		return fmt.Errorf("failed to migrate tasks: %w", err)
	}

	// Step 5: Migrate summaries
	if err := m.migrateSummaries(ctx); err != nil {
		return fmt.Errorf("failed to migrate summaries: %w", err)
	}

	// Step 6: Report
	m.reportStats()

	return nil
}

// migrateUser reads user from SPC and creates in NoteBridge.
func (m *Migrator) migrateUser(ctx context.Context) error {
	user, err := m.spcReader.ReadUser(ctx)
	if err != nil {
		return err
	}

	if !m.dryRun {
		if err := m.syncStore.EnsureUser(ctx, user.Email, user.PasswordHash, user.UserID); err != nil {
			return fmt.Errorf("failed to ensure user: %w", err)
		}

		// Get the user to retrieve its ID
		createdUser, err := m.syncStore.GetUserByEmail(ctx, user.Email)
		if err != nil {
			return fmt.Errorf("failed to retrieve created user: %w", err)
		}

		// Create equipment entry (default equipment)
		if err := m.syncStore.EnsureEquipment(ctx, "default", createdUser.ID); err != nil {
			return fmt.Errorf("failed to create equipment: %w", err)
		}
	}

	m.stats.UsersMigrated++
	m.logger.Info("Migrated user", "email", user.Email)
	return nil
}

// migrateFiles reads files from SPC, creates directories, and copies files.
func (m *Migrator) migrateFiles(ctx context.Context) error {
	// Get user ID
	user, err := m.spcReader.ReadUser(ctx)
	if err != nil {
		return err
	}

	var userID int64
	if !m.dryRun {
		createdUser, err := m.syncStore.GetUserByEmail(ctx, user.Email)
		if err != nil {
			return err
		}
		userID = createdUser.ID
	} else {
		userID = 1 // Dummy ID for dry run
	}

	// Read all files from SPC
	spcFiles, err := m.spcReader.ReadFiles(ctx, user.UserID)
	if err != nil {
		return err
	}

	// Define SPC root folder IDs
	rootFolderIDs := map[int64]string{
		1: "DOCUMENT",
		2: "NOTE",
		3: "EXPORT",
		4: "SCREENSHOT",
		5: "INBOX",
	}

	// Process folders first (due to SPC ordering)
	for _, spcFile := range spcFiles {
		if !spcFile.IsFolder {
			continue
		}

		nid := m.snowflake.Generate()
		m.dirMap[spcFile.ID] = nid

		if !m.dryRun {
			// Determine parent NoteBridge ID: root folders use 0, others look up parent
			parentNID := int64(0)
			if spcFile.DirectoryID != 0 {
				parentNID = m.dirMap[spcFile.DirectoryID]
			}

			entry := &syncdb.FileEntry{
				ID:          nid,
				UserID:      userID,
				DirectoryID: parentNID,
				FileName:    spcFile.FileName,
				InnerName:   spcFile.InnerName,
				IsFolder:    true,
				IsActive:    true,
			}

			if rootName, isRoot := rootFolderIDs[spcFile.ID]; isRoot {
				entry.FileName = rootName
			}

			if err := m.syncStore.CreateFile(ctx, entry); err != nil {
				return fmt.Errorf("failed to create folder: %w", err)
			}
		}

		m.stats.FoldersMigrated++
		m.logger.Debug("Migrated folder", "spcID", spcFile.ID, "name", spcFile.FileName)
	}

	// Build a map from SPC folder ID to folder path by walking directory_id tree
	folderPaths := make(map[int64]string)
	folderPaths[0] = "" // Root has empty path

	for _, spcFile := range spcFiles {
		if spcFile.IsFolder {
			var folderPath string
			if spcFile.DirectoryID == 0 {
				// Root folder
				folderPath = spcFile.FileName
			} else {
				// Subfolder: parent path + this folder name
				parentPath, _ := folderPaths[spcFile.DirectoryID]
				folderPath = filepath.Join(parentPath, spcFile.FileName)
			}
			folderPaths[spcFile.ID] = folderPath
		}
	}

	// Process files
	for _, spcFile := range spcFiles {
		if spcFile.IsFolder {
			continue
		}

		// Get parent directory ID from mapping
		parentNID := m.dirMap[spcFile.DirectoryID]

		nid := m.snowflake.Generate()
		storageKey := fmt.Sprintf("files/%d", nid)

		// Reconstruct source path using folder path from tree
		folderPath, _ := folderPaths[spcFile.DirectoryID]
		sourcePath := filepath.Join(m.spcPath, user.Email, "Supernote", folderPath, spcFile.InnerName)
		fileSize, md5Hash, err := m.copyFile(ctx, sourcePath, storageKey)
		if err != nil {
			m.stats.MissingFiles++
			m.logger.Warn("Failed to copy file", "path", sourcePath, "error", err)
			continue // Continue despite missing file
		}

		// Verify MD5
		if md5Hash != spcFile.MD5 {
			m.stats.MD5Mismatches++
			m.logger.Warn("MD5 mismatch", "file", spcFile.FileName, "expected", spcFile.MD5, "got", md5Hash)
		}

		if !m.dryRun {
			entry := &syncdb.FileEntry{
				ID:          nid,
				UserID:      userID,
				DirectoryID: parentNID,
				FileName:    spcFile.FileName,
				InnerName:   spcFile.InnerName,
				StorageKey:  storageKey,
				MD5:         md5Hash,
				Size:        fileSize,
				IsFolder:    false,
				IsActive:    true,
			}

			if err := m.syncStore.CreateFile(ctx, entry); err != nil {
				return fmt.Errorf("failed to create file entry: %w", err)
			}
		}

		m.stats.FilesMigrated++
		m.stats.BytesMigrated += fileSize
		m.logger.Debug("Migrated file", "spcID", spcFile.ID, "name", spcFile.FileName, "size", fileSize)
	}

	return nil
}

// migrateTaskGroups reads task groups from SPC and creates in NoteBridge.
func (m *Migrator) migrateTaskGroups(ctx context.Context) error {
	// Get user ID
	user, err := m.spcReader.ReadUser(ctx)
	if err != nil {
		return err
	}

	var userID int64
	if !m.dryRun {
		createdUser, err := m.syncStore.GetUserByEmail(ctx, user.Email)
		if err != nil {
			return err
		}
		userID = createdUser.ID
	} else {
		userID = 1 // Dummy ID for dry run
	}

	groups, err := m.spcReader.ReadTaskGroups(ctx, user.UserID)
	if err != nil {
		return err
	}

	for _, g := range groups {
		if !m.dryRun {
			group := &syncdb.ScheduleGroup{
				TaskListID:   g.TaskListID,
				UserID:       userID,
				Title:        g.Title,
				LastModified: g.LastModified,
				CreateTime:   g.LastModified, // Use same for creation
			}

			if err := m.syncStore.UpsertScheduleGroup(ctx, group); err != nil {
				return fmt.Errorf("failed to create task group: %w", err)
			}
		}

		m.stats.TaskGroupsMigrated++
		m.logger.Debug("Migrated task group", "title", g.Title)
	}

	return nil
}

// migrateTasks reads tasks from SPC and creates in NoteBridge.
func (m *Migrator) migrateTasks(ctx context.Context) error {
	// Get user ID
	user, err := m.spcReader.ReadUser(ctx)
	if err != nil {
		return err
	}

	var userID int64
	if !m.dryRun {
		createdUser, err := m.syncStore.GetUserByEmail(ctx, user.Email)
		if err != nil {
			return err
		}
		userID = createdUser.ID
	} else {
		userID = 1 // Dummy ID for dry run
	}

	tasks, err := m.spcReader.ReadTasks(ctx, user.UserID)
	if err != nil {
		return err
	}

	for _, t := range tasks {
		if !m.dryRun {
			task := &syncdb.ScheduleTask{
				TaskID:       t.TaskID,
				UserID:       userID,
				TaskListID:   t.TaskListID,
				Title:        t.Title,
				Detail:       t.Detail,
				Status:       t.Status,
				Importance:   t.Importance,
				Recurrence:   t.Recurrence,
				Links:        t.Links,
				IsReminderOn: t.IsReminderOn,
				DueTime:      t.DueTime,
				LastModified: t.LastModified,
			}

			// Only set CompletedTime when task is completed
			if t.Status == "completed" {
				task.CompletedTime = t.LastModified
			}

			if err := m.syncStore.UpsertScheduleTask(ctx, task); err != nil {
				return fmt.Errorf("failed to create task: %w", err)
			}
		}

		m.stats.TasksMigrated++
		m.logger.Debug("Migrated task", "title", t.Title)
	}

	return nil
}

// migrateSummaries reads summaries from SPC and creates in NoteBridge.
func (m *Migrator) migrateSummaries(ctx context.Context) error {
	// Get user ID
	user, err := m.spcReader.ReadUser(ctx)
	if err != nil {
		return err
	}

	var userID int64
	if !m.dryRun {
		createdUser, err := m.syncStore.GetUserByEmail(ctx, user.Email)
		if err != nil {
			return err
		}
		userID = createdUser.ID
	} else {
		userID = 1 // Dummy ID for dry run
	}

	summaries, err := m.spcReader.ReadSummaries(ctx, user.UserID)
	if err != nil {
		return err
	}

	for _, s := range summaries {
		nid := m.snowflake.Generate()

		// Copy handwrite file if present
		if s.HandwriteInnerName != "" {
			sourcePath := filepath.Join(m.spcPath, user.Email, "Supernote", "summaries", s.HandwriteInnerName)
			storageKey := fmt.Sprintf("summaries/%s", s.HandwriteInnerName)
			_, _, err := m.copyFile(ctx, sourcePath, storageKey)
			if err != nil {
				m.stats.HandwriteFilesMissing++
				m.logger.Warn("Failed to copy handwrite file", "path", sourcePath, "error", err)
			} else {
				m.stats.HandwriteFilesCopied++
			}
		}

		if !m.dryRun {
			summary := &syncdb.Summary{
				ID:                     nid,
				UserID:                 userID,
				UniqueIdentifier:       s.UniqueIdentifier,
				Name:                   s.Name,
				Description:            s.Description,
				FileID:                 s.FileID,
				ParentUniqueIdentifier: s.ParentUniqueIdentifier,
				Content:                s.Content,
				DataSource:             s.DataSource,
				SourcePath:             s.SourcePath,
				SourceType:             parseSourceType(s.SourceType),
				Tags:                   s.Tags,
				MD5Hash:                s.MD5Hash,
				HandwriteInnerName:     s.HandwriteInnerName,
				Metadata:               s.Metadata,
				IsSummaryGroup:         func() string { if s.IsSummaryGroup { return "Y" } else { return "N" } }(),
				Author:                 s.Author,
				CreationTime:           s.CreationTime,
				LastModifiedTime:       s.LastModifiedTime,
			}

			if err := m.syncStore.CreateSummary(ctx, summary); err != nil {
				return fmt.Errorf("failed to create summary: %w", err)
			}
		}

		m.stats.SummariesMigrated++
		m.logger.Debug("Migrated summary", "name", s.Name)
	}

	return nil
}

// copyFile copies a file from SPC to blob storage, computing MD5.
func (m *Migrator) copyFile(ctx context.Context, sourcePath, storageKey string) (int64, string, error) {
	// Try to open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return 0, "", fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	// If dry run, just compute hash without writing
	if m.dryRun {
		md5Hash := md5.New()
		size, err := io.Copy(md5Hash, sourceFile)
		if err != nil {
			return 0, "", fmt.Errorf("failed to read source file: %w", err)
		}
		return size, fmt.Sprintf("%x", md5Hash.Sum(nil)), nil
	}

	// For real migration, use blob store's Put method which handles MD5
	size, md5Hash, err := m.blobStore.Put(ctx, storageKey, sourceFile)
	if err != nil {
		return 0, "", fmt.Errorf("failed to put blob: %w", err)
	}

	return size, md5Hash, nil
}

// reportStats logs migration summary.
func (m *Migrator) reportStats() {
	m.logger.Info("Migration complete",
		"users", m.stats.UsersMigrated,
		"folders", m.stats.FoldersMigrated,
		"files", m.stats.FilesMigrated,
		"bytes", m.stats.BytesMigrated,
		"taskGroups", m.stats.TaskGroupsMigrated,
		"tasks", m.stats.TasksMigrated,
		"summaries", m.stats.SummariesMigrated,
		"handwriteFilesCopied", m.stats.HandwriteFilesCopied,
	)

	if m.stats.MD5Mismatches > 0 {
		m.logger.Warn("MD5 mismatches detected", "count", m.stats.MD5Mismatches)
	}
	if m.stats.MissingFiles > 0 {
		m.logger.Warn("Missing files encountered", "count", m.stats.MissingFiles)
	}
	if m.stats.HandwriteFilesMissing > 0 {
		m.logger.Warn("Missing handwrite files", "count", m.stats.HandwriteFilesMissing)
	}
}

func parseSourceType(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
