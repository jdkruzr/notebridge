package sync

import (
	"log/slog"
	"net/http"

	"github.com/sysop/notebridge/internal/blob"
	"github.com/sysop/notebridge/internal/syncdb"
)

// Server handles HTTP routing and middleware for the sync API.
type Server struct {
	store       *syncdb.Store
	authService *AuthService
	blobStore   blob.BlobStore
	chunkStore  *blob.ChunkStore
	snowflake   *SnowflakeGenerator
	logger      *slog.Logger
}

// NewServer creates a new Server instance.
func NewServer(
	store *syncdb.Store,
	authService *AuthService,
	blobStore blob.BlobStore,
	chunkStore *blob.ChunkStore,
	snowflake *SnowflakeGenerator,
	logger *slog.Logger,
) *Server {
	return &Server{
		store:       store,
		authService: authService,
		blobStore:   blobStore,
		chunkStore:  chunkStore,
		snowflake:   snowflake,
		logger:      logger,
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
