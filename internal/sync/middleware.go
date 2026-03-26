package sync

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Context keys for storing auth information
type contextKey string

const (
	contextKeyUserID      contextKey = "userID"
	contextKeyEquipmentNo contextKey = "equipmentNo"
)

// AuthMiddleware extracts and validates JWT tokens from Authorization headers.
// Stores userID and equipmentNo in request context.
// Skips auth for public endpoints: challenge requests and login verification.
func AuthMiddleware(authService *AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for public endpoints
			if isPublicEndpoint(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token: try x-access-token header first (SPC protocol),
			// then fall back to Authorization: Bearer (standard HTTP)
			token := r.Header.Get("x-access-token")
			if token == "" {
				authHeader := r.Header.Get("Authorization")
				if authHeader != "" {
					parts := strings.SplitN(authHeader, " ", 2)
					if len(parts) == 2 && parts[0] == "Bearer" {
						token = parts[1]
					}
				}
			}
			if token == "" {
				jsonError(w, ErrInvalidToken())
				return
			}

			// Validate token
			userID, equipmentNo, err := authService.ValidateJWTToken(r.Context(), token)
			if err != nil {
				syncErr, ok := err.(*SyncError)
				if ok {
					jsonError(w, syncErr)
				} else {
					jsonError(w, ErrInvalidToken())
				}
				return
			}

			// Store in context and proceed
			ctx := context.WithValue(r.Context(), contextKeyUserID, userID)
			ctx = context.WithValue(ctx, contextKeyEquipmentNo, equipmentNo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// isPublicEndpoint returns true if the path is a public endpoint that doesn't require auth.
// OSS endpoints use signed URL verification instead of JWT auth.
// Socket.IO performs its own JWT validation via query params.
func isPublicEndpoint(path string) bool {
	// Add public endpoints here as needed
	publicPaths := map[string]bool{
		// SPC-compatible auth paths (tablet uses these)
		"/api/file/query/server":                       true,
		"/api/official/user/query/random/code":          true,
		"/api/official/user/account/login/equipment":    true,
		"/api/official/user/check/exists/server":        true,
		"/api/terminal/user/bindEquipment":              true,
		"/api/terminal/equipment/unlink":                true,
		// Legacy auth paths
		"/api/user/login/challenge": true,
		"/api/user/login/verify":    true,
		// Infrastructure
		"/health":                   true,
		"/api/oss/download":         true, // Signed URL verification instead of JWT
		"/api/oss/upload":           true, // Signed URL verification instead of JWT
		"/api/oss/upload/part":      true, // Signed URL verification instead of JWT
		"/socket.io/":               true, // Socket.IO performs its own JWT validation via query params
	}
	return publicPaths[path]
}

// RecoveryMiddleware recovers from panics, logs the stack trace, and returns 500.
func RecoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log the panic and stack trace
					logger.Error("panic recovered",
						slog.String("error", sprint(err)),
						slog.String("stack", string(debug.Stack())),
					)

					// Return 500 error
					jsonError(w, ErrInternal(sprint(err)))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs all HTTP requests with method, path, status, and duration.
func LoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Measure request duration
			start := time.Now()
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			// Log request
			logger.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", duration),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// WriteHeader captures the status code.
func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through middleware.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not support hijacking")
}

// Flush implements http.Flusher for streaming responses.
func (w *responseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// UserIDFromContext extracts userID from request context.
// Returns 0 if not found.
func UserIDFromContext(ctx context.Context) int64 {
	userID, ok := ctx.Value(contextKeyUserID).(int64)
	if ok {
		return userID
	}
	return 0
}

// EquipmentNoFromContext extracts equipmentNo from request context.
// Returns empty string if not found.
func EquipmentNoFromContext(ctx context.Context) string {
	equipmentNo, ok := ctx.Value(contextKeyEquipmentNo).(string)
	if ok {
		return equipmentNo
	}
	return ""
}

// sprint converts an interface to string safely.
func sprint(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case error:
		return x.Error()
	default:
		// Try to marshal as JSON, fall back to %v
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
		return "unknown error"
	}
}
