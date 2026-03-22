package sync

import (
	"log/slog"
	"net/http"

	"github.com/sysop/notebridge/internal/syncdb"
)

// Server handles HTTP routing and middleware for the sync API.
type Server struct {
	store       *syncdb.Store
	authService *AuthService
	logger      *slog.Logger
}

// NewServer creates a new Server instance.
func NewServer(store *syncdb.Store, authService *AuthService, logger *slog.Logger) *Server {
	return &Server{
		store:       store,
		authService: authService,
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

	// Wrap with middleware chain (innermost first, then wrap with next)
	// Order: mux -> AuthMiddleware -> LoggingMiddleware -> RecoveryMiddleware
	var handler http.Handler = mux

	// Apply AuthMiddleware
	handler = AuthMiddleware(s.authService)(handler)

	// Apply LoggingMiddleware
	handler = LoggingMiddleware(s.logger)(handler)

	// Apply RecoveryMiddleware (outermost)
	handler = RecoveryMiddleware(s.logger)(handler)

	return handler
}
