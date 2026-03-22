package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/config"
	"github.com/sysop/notebridge/internal/events"
	"github.com/sysop/notebridge/internal/notestore"
	"github.com/sysop/notebridge/internal/pipeline"
	"github.com/sysop/notebridge/internal/processor"
	"github.com/sysop/notebridge/internal/search"
	"github.com/sysop/notebridge/internal/sync"
	"github.com/sysop/notebridge/internal/syncdb"
)

func main() {
	// Load config from environment
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Set up structured logging (JSON handler for now)
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		logLevel = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Open SQLite database
	db, err := syncdb.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("database opened", "path", cfg.DBPath)

	// Create store
	store := syncdb.NewStore(db)

	// Bootstrap user with EnsureUser
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := store.EnsureUser(ctx, cfg.UserEmail, cfg.UserPasswordHash, nil); err != nil {
		logger.Error("failed to bootstrap user", "error", err)
		os.Exit(1)
	}

	logger.Info("user bootstrapped", "email", cfg.UserEmail)

	// Get or create JWT secret from store
	// AuthService will call GetOrCreateJWTSecret as needed
	_, err = store.GetOrCreateJWTSecret(ctx)
	if err != nil {
		logger.Error("failed to get or create JWT secret", "error", err)
		os.Exit(1)
	}

	// Create SnowflakeGenerator
	snowflake := sync.NewSnowflakeGenerator()

	// Create AuthService
	authService := sync.NewAuthService(store, snowflake)

	// Create BlobStore (local filesystem)
	blobStore := blob.NewLocalStore(cfg.BlobStorePath)

	// Create ChunkStore (for temporary chunk storage during multipart uploads)
	chunkStore := blob.NewChunkStore(cfg.ChunkStorePath)

	// Create EventBus for file change events
	eventBus := events.NewEventBus()

	// Create NotifyManager for WebSocket client notifications
	notifier := sync.NewNotifyManager()

	// Create sync.Server
	server := sync.NewServer(store, authService, blobStore, chunkStore, snowflake, logger, eventBus, notifier)

	// Subscribe notifier to file events and broadcast ServerMessage to connected clients
	eventTypes := []string{events.FileUploaded, events.FileModified, events.FileDeleted}
	for _, eventType := range eventTypes {
		eventBus.Subscribe(eventType, func(e events.Event) {
			payload, err := buildServerMessage(e)
			if err != nil {
				logger.Error("failed to build server message", "error", err, "event", e.Type)
				return
			}
			notifier.NotifyUser(e.UserID, payload)
		})
	}

	// Get the user ID for pipeline operations
	user, err := store.GetUserByEmail(ctx, cfg.UserEmail)
	if err != nil {
		logger.Error("failed to get user", "error", err)
		os.Exit(1)
	}
	if user == nil {
		logger.Error("user not found after bootstrap")
		os.Exit(1)
	}
	userID := user.ID

	// Create OCR client if enabled
	var ocrClient *processor.OCRClient
	if cfg.OCREnabled {
		ocrClient = processor.NewOCRClient(cfg.OCRAPIURL, cfg.OCRAPIKey, cfg.OCRModel, cfg.OCRAPIFormat)
	}

	// Create NoteStore for pipeline
	noteStore := notestore.New(db, cfg.StoragePath)

	// Create SearchIndex for pipeline
	searchIndex := search.New(db)

	// Create Processor with AfterInject hook
	workerCfg := processor.WorkerConfig{
		OCREnabled: cfg.OCREnabled,
		BackupPath: cfg.BackupPath,
		MaxFileMB:  cfg.OCRMaxFileMB,
		OCRClient:  ocrClient,
		Indexer:    searchIndex,
		AfterInject: func(ctx context.Context, path, md5 string, size int64) error {
			// Convert absolute path to storage key
			storageKey := storageKeyFromPath(path, cfg.StoragePath)

			// Find the file entry in syncdb by matching the storage path
			fileEntry, err := store.GetFileByStorageKey(ctx, userID, storageKey)
			if err != nil {
				return fmt.Errorf("lookup file: %w", err)
			}
			if fileEntry == nil {
				return fmt.Errorf("file not found in syncdb: %s", storageKey)
			}

			// Update syncdb with new MD5 and size
			if err := store.UpdateFileMD5(ctx, fileEntry.ID, md5, size); err != nil {
				return fmt.Errorf("update file: %w", err)
			}

			// Publish FileModifiedEvent for Socket.IO notification
			eventBus.Publish(events.Event{
				Type:   events.FileModified,
				FileID: fileEntry.ID,
				UserID: fileEntry.UserID,
				Path:   path,
			})

			return nil
		},
	}

	proc := processor.New(db, workerCfg)

	// Start processor
	if err := proc.Start(ctx); err != nil {
		logger.Error("failed to start processor", "error", err)
		os.Exit(1)
	}
	defer proc.Stop()

	// Create and start Pipeline
	pipe := pipeline.New(pipeline.Config{
		NotesPath: cfg.StoragePath,
		NoteStore: noteStore,
		Processor: proc,
		EventBus:  eventBus,
		Logger:    logger,
	})
	pipe.Start(ctx)
	defer pipe.Close()

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         cfg.SyncListenAddr,
		Handler:      server.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		logger.Info("starting sync server", "addr", cfg.SyncListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
		}
	}()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("received signal", "signal", sig)

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown http server", "error", err)
		os.Exit(1)
	}

	logger.Info("sync server stopped gracefully")
}

// storageKeyFromPath converts an absolute blob path to the storage key.
// The storage key is the relative path from the storage root.
func storageKeyFromPath(absPath, storageRoot string) string {
	// Normalize paths to handle trailing slashes
	absPath = filepath.Clean(absPath)
	storageRoot = filepath.Clean(storageRoot)

	// If the path is within the storage root, return the relative path
	if relPath, err := filepath.Rel(storageRoot, absPath); err == nil {
		return relPath
	}

	// Fallback: return the path as-is if not within storage root
	return absPath
}

// buildServerMessage constructs a ServerMessage payload for Socket.IO notification.
// Format matches SPC: {"code":"200","timestamp":<ms>,"msgType":"FILE-SYN","data":[{"messageType":"STARTSYNC","equipmentNo":"notebridge","timestamp":<ms>}]}
func buildServerMessage(e events.Event) (string, error) {
	now := time.Now().UnixMilli()
	payload := map[string]interface{}{
		"code":      "200",
		"timestamp": now,
		"msgType":   "FILE-SYN",
		"data": []map[string]interface{}{
			{
				"messageType": "STARTSYNC",
				"equipmentNo": "notebridge",
				"timestamp":   now,
			},
		},
	}
	// Use sync's EncodeEvent to wrap it as a Socket.IO event
	return sync.EncodeEvent("ServerMessage", payload)
}
