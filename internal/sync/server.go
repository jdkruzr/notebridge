package sync

import (
	"log/slog"
	"net/http"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/events"
	"github.com/sysop/notebridge/internal/syncdb"
	"golang.org/x/net/websocket"
)

// Server handles HTTP routing and middleware for the sync API.
type Server struct {
	store       *syncdb.Store
	authService *AuthService
	blobStore   blob.BlobStore
	chunkStore  *blob.ChunkStore
	snowflake   *SnowflakeGenerator
	logger      *slog.Logger
	eventBus    *events.EventBus
	notifier    *NotifyManager
}

// NewServer creates a new Server instance.
func NewServer(
	store *syncdb.Store,
	authService *AuthService,
	blobStore blob.BlobStore,
	chunkStore *blob.ChunkStore,
	snowflake *SnowflakeGenerator,
	logger *slog.Logger,
	eventBus *events.EventBus,
	notifier *NotifyManager,
) *Server {
	return &Server{
		store:       store,
		authService: authService,
		blobStore:   blobStore,
		chunkStore:  chunkStore,
		snowflake:   snowflake,
		logger:      logger,
		eventBus:    eventBus,
		notifier:    notifier,
	}
}

// Handler returns the fully-wired HTTP handler with middleware chain.
// Middleware order (outermost to innermost): Recovery -> Logging -> Auth -> Routes
func (s *Server) Handler() http.Handler {
	// Create a new mux for routing
	mux := http.NewServeMux()

	// Public endpoints (no auth required)
	mux.HandleFunc("POST /api/user/login/challenge", s.handleChallenge)
	mux.HandleFunc("POST /api/user/login/verify", s.handleLoginVerify)
	mux.HandleFunc("GET /health", s.handleHealth)

	// OSS endpoints (signature-verified, treated as public)
	mux.HandleFunc("GET /api/oss/download", s.handleOssDownload)
	mux.HandleFunc("POST /api/oss/upload", s.handleOssUpload)
	mux.HandleFunc("POST /api/oss/upload/part", s.handleOssUploadPart)

	// Sync lock endpoints (auth required)
	mux.HandleFunc("POST /api/file/2/files/synchronous/start", s.handleSyncStart)
	mux.HandleFunc("POST /api/file/2/files/synchronous/end", s.handleSyncEnd)

	// Folder endpoints (auth required)
	mux.HandleFunc("POST /api/file/2/files/create_folder_v2", s.handleCreateFolder)
	mux.HandleFunc("POST /api/file/3/files/list_folder_v3", s.handleListFolderV3)
	mux.HandleFunc("POST /api/file/2/files/list_folder", s.handleListFolderV2)

	// Upload endpoints (auth required for apply/finish, signed URL for PUT)
	mux.HandleFunc("POST /api/file/3/files/upload/apply", s.handleUploadApply)
	mux.HandleFunc("POST /api/file/2/files/upload/finish", s.handleUploadFinish)

	// Download endpoint (auth required)
	mux.HandleFunc("POST /api/file/3/files/download_v3", s.handleDownloadV3)

	// File operation endpoints (auth required)
	mux.HandleFunc("POST /api/file/3/files/delete_folder_v3", s.handleDeleteV3)
	mux.HandleFunc("POST /api/file/3/files/query_v3", s.handleQueryV3)
	mux.HandleFunc("POST /api/file/3/files/query/by/path_v3", s.handleQueryByPathV3)
	mux.HandleFunc("POST /api/file/3/files/move_v3", s.handleMoveV3)
	mux.HandleFunc("POST /api/file/3/files/copy_v3", s.handleCopyV3)
	mux.HandleFunc("POST /api/file/3/files/space_usage", s.handleSpaceUsage)

	// Task endpoints (auth required)
	mux.HandleFunc("POST /api/file/schedule/group", s.handleCreateScheduleGroup)
	mux.HandleFunc("PUT /api/file/schedule/group", s.handleUpdateScheduleGroup)
	mux.HandleFunc("DELETE /api/file/schedule/group/{taskListId}", s.handleDeleteScheduleGroup)
	mux.HandleFunc("POST /api/file/schedule/group/all", s.handleListScheduleGroups)
	mux.HandleFunc("POST /api/file/schedule/task", s.handleCreateScheduleTask)
	mux.HandleFunc("PUT /api/file/schedule/task/list", s.handleBatchUpdateTasks)
	mux.HandleFunc("DELETE /api/file/schedule/task/{taskId}", s.handleDeleteScheduleTask)
	mux.HandleFunc("POST /api/file/schedule/task/all", s.handleListScheduleTasks)

	// Digest endpoints (auth required)
	mux.HandleFunc("POST /api/file/add/summary/group", s.handleCreateSummaryGroup)
	mux.HandleFunc("PUT /api/file/update/summary/group", s.handleUpdateSummaryGroup)
	mux.HandleFunc("DELETE /api/file/delete/summary/group", s.handleDeleteSummaryGroup)
	mux.HandleFunc("POST /api/file/query/summary/group", s.handleListSummaryGroups)
	mux.HandleFunc("POST /api/file/add/summary", s.handleCreateSummary)
	mux.HandleFunc("PUT /api/file/update/summary", s.handleUpdateSummary)
	mux.HandleFunc("DELETE /api/file/delete/summary", s.handleDeleteSummary)
	mux.HandleFunc("POST /api/file/query/summary/hash", s.handleQuerySummaryHash)
	mux.HandleFunc("POST /api/file/query/summary/id", s.handleQuerySummaryByIDs)
	mux.HandleFunc("POST /api/file/query/summary", s.handleQuerySummaries)
	mux.HandleFunc("POST /api/file/upload/apply/summary", s.handleUploadSummaryApply)
	mux.HandleFunc("POST /api/file/download/summary", s.handleDownloadSummary)

	// Socket.IO WebSocket endpoint (handles its own auth via query params)
	mux.Handle("/socket.io/", websocket.Handler(SocketIOHandler(s.authService, s.notifier, s.logger)))

	// Wrap with middleware chain (innermost first, then wrap with next)
	// Order: mux -> AuthMiddleware -> LoggingMiddleware -> RecoveryMiddleware
	var handler http.Handler = mux

	// Apply AuthMiddleware (skips public endpoints internally)
	handler = AuthMiddleware(s.authService)(handler)

	// Apply LoggingMiddleware
	handler = LoggingMiddleware(s.logger)(handler)

	// Apply RecoveryMiddleware (outermost)
	handler = RecoveryMiddleware(s.logger)(handler)

	return handler
}
