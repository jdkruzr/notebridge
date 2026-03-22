package web

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/sysop/notebridge/internal/logging"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Origin restriction is unnecessary because the /ws/logs endpoint is protected
		// by the Basic Auth middleware. Only authenticated users can reach the WebSocket upgrade.
		return true
	},
}

// RegisterLogStreamHandler registers the WebSocket log streaming endpoint
// on the mux.
func (h *Handler) registerLogStreamHandler(broadcaster *logging.LogBroadcaster) {
	h.mux.HandleFunc("GET /ws/logs", func(w http.ResponseWriter, r *http.Request) {
		h.handleWebSocketLogs(w, r, broadcaster)
	})
}

// handleWebSocketLogs upgrades the HTTP connection to WebSocket and streams
// log entries with optional level filtering.
func (h *Handler) handleWebSocketLogs(w http.ResponseWriter, r *http.Request, broadcaster *logging.LogBroadcaster) {
	// Get log level filter from query parameter
	levelStr := strings.ToLower(r.URL.Query().Get("level"))
	if levelStr == "" {
		levelStr = "info"
	}
	minLevel := parseLogLevel(levelStr)

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade websocket", "error", err)
		return
	}
	defer conn.Close()

	// Subscribe to log entries and unsubscribe on disconnect
	subscriberID, logChan := broadcaster.Subscribe()
	defer broadcaster.Unsubscribe(subscriberID)

	// Send log entries to the WebSocket client
	for entry := range logChan {
		// Filter by level
		if !shouldIncludeLogEntry(entry, minLevel) {
			continue
		}

		// Send to WebSocket client
		if err := conn.WriteMessage(websocket.TextMessage, []byte(entry)); err != nil {
			h.logger.Warn("failed to write log to websocket", "error", err)
			return
		}
	}
}

// parseLogLevel converts a string to slog.Level.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// shouldIncludeLogEntry checks if a log entry should be included based on
// the minimum level filter.
func shouldIncludeLogEntry(entry string, minLevel slog.Level) bool {
	// Parse the log level from the entry format: "15:04:05 [LEVEL] MESSAGE"
	// Expected format: "15:04:05 [DEBUG] ...", "15:04:05 [INFO] ...", etc.

	// Find the opening bracket
	openIdx := strings.Index(entry, "[")
	if openIdx == -1 {
		return true // No bracket found, include entry
	}

	// Find the closing bracket after the opening bracket
	closeIdx := strings.Index(entry[openIdx+1:], "]")
	if closeIdx == -1 {
		return true // No closing bracket found, include entry
	}

	// Extract the level string between brackets
	levelStr := strings.ToUpper(strings.TrimSpace(entry[openIdx+1 : openIdx+1+closeIdx]))
	if levelStr == "" {
		return true // Empty level string, include entry
	}

	entryLevel := parseLogLevel(levelStr)

	// Include if entry level >= minLevel
	return entryLevel >= minLevel
}
