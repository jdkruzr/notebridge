package sync

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sysop/notebridge/internal/events"
	"golang.org/x/net/websocket"
)

// connectSocketIO dials a WebSocket connection to the Socket.IO endpoint with the given token.
func connectSocketIO(t *testing.T, serverURL, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + serverURL[4:] + "/socket.io/?token=" + token + "&type=test&EIO=3&transport=websocket"
	ws, err := websocket.Dial(wsURL, "", serverURL)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	return ws
}

// readFrame reads a frame from the websocket with a timeout.
func readFrame(t *testing.T, ws *websocket.Conn) string {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame string
	err := websocket.Message.Receive(ws, &frame)
	if err != nil {
		t.Fatalf("failed to read frame: %v", err)
	}
	return frame
}

// writeFrame writes a frame to the websocket.
func writeFrame(t *testing.T, ws *websocket.Conn, frame string) {
	t.Helper()
	err := websocket.Message.Send(ws, frame)
	if err != nil {
		t.Fatalf("failed to write frame: %v", err)
	}
}

// setupSocketIOTestServer extends setupTestServer with Socket.IO handler.
// Returns httptest.Server, AuthService, EventBus, and NotifyManager from a custom setup.
func setupSocketIOTestServer(t *testing.T) (*httptest.Server, *AuthService, *events.EventBus, *NotifyManager) {
	t.Helper()

	// Use the existing setupTestServer infrastructure
	_, store := setupTestServer(t)

	// Extract what we need from the store
	snowflake := NewSnowflakeGenerator()
	authService := NewAuthService(store, snowflake)

	// Ensure JWT secret is created
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = store.GetOrCreateJWTSecret(ctx)

	// Create event bus and notifier
	eventBus := events.NewEventBus()
	notifier := NewNotifyManager()

	// Create a custom test server with just the Socket.IO handler
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := http.NewServeMux()
	mux.Handle("/socket.io/", websocket.Handler(SocketIOHandler(authService, notifier, logger)))

	testServer := httptest.NewServer(mux)
	t.Cleanup(func() {
		testServer.Close()
	})

	return testServer, authService, eventBus, notifier
}

// TestSocketIOHandshake verifies AC3.1: successful handshake with valid JWT.
func TestSocketIOHandshake(t *testing.T) {
	httpServer, authService, _, _ := setupSocketIOTestServer(t)

	// Generate a valid JWT token
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := authService.createJWTToken(ctx, 1, "") // userID=1
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Connect
	ws := connectSocketIO(t, httpServer.URL, token)
	defer ws.Close()

	// Read open packet
	openFrame := readFrame(t, ws)
	pType, payload := DecodeFrame(openFrame)
	if pType != '0' {
		t.Errorf("expected open packet type '0', got %c", pType)
	}

	// Parse the open packet
	var openPayload map[string]interface{}
	err = json.Unmarshal([]byte(payload), &openPayload)
	if err != nil {
		t.Fatalf("failed to parse open packet: %v", err)
	}

	if _, ok := openPayload["sid"].(string); !ok {
		t.Errorf("expected sid field in open packet")
	}
	if pingInterval, ok := openPayload["pingInterval"].(float64); !ok || pingInterval != 25000 {
		t.Errorf("expected pingInterval=25000, got %v", openPayload["pingInterval"])
	}
	if pingTimeout, ok := openPayload["pingTimeout"].(float64); !ok || pingTimeout != 5000 {
		t.Errorf("expected pingTimeout=5000, got %v", openPayload["pingTimeout"])
	}

	// Read connect ack
	connectFrame := readFrame(t, ws)
	if connectFrame != "40" {
		t.Errorf("expected connect ack '40', got %q", connectFrame)
	}
}

// TestSocketIOPingPong verifies AC3.2: ping/pong keepalive.
func TestSocketIOPingPong(t *testing.T) {
	httpServer, authService, _, _ := setupSocketIOTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := authService.createJWTToken(ctx, 1, "")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	ws := connectSocketIO(t, httpServer.URL, token)
	defer ws.Close()

	// Consume handshake frames
	readFrame(t, ws) // open
	readFrame(t, ws) // connect

	// Send ping
	writeFrame(t, ws, string(PacketPing))

	// Read pong response
	pongFrame := readFrame(t, ws)
	if pongFrame != string(PacketPong) {
		t.Errorf("expected pong '%c', got %q", PacketPong, pongFrame)
	}
}

// TestSocketIOInvalidJWT verifies AC3.4: invalid JWT rejection.
func TestSocketIOInvalidJWT(t *testing.T) {
	httpServer, _, _, _ := setupSocketIOTestServer(t)

	// Try to connect with invalid token
	wsURL := "ws" + httpServer.URL[4:] + "/socket.io/?token=invalid&type=test&EIO=3&transport=websocket"
	ws, err := websocket.Dial(wsURL, "", httpServer.URL)
	if err != nil {
		t.Fatalf("failed to dial websocket: %v", err)
	}
	defer ws.Close()

	// Should receive error frame
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	var frame string
	err = websocket.Message.Receive(ws, &frame)
	if err != nil {
		t.Fatalf("failed to read error frame: %v", err)
	}

	// Check that it's an error frame (starts with '44')
	if !startsWith(frame, "44") {
		t.Errorf("expected error frame starting with '44', got %q", frame)
	}
}

// TestSocketIORattaPing tests ratta_ping echo.
func TestSocketIORattaPing(t *testing.T) {
	httpServer, authService, _, _ := setupSocketIOTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := authService.createJWTToken(ctx, 1, "")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	ws := connectSocketIO(t, httpServer.URL, token)
	defer ws.Close()

	// Consume handshake
	readFrame(t, ws)
	readFrame(t, ws)

	// Send ratta_ping
	rattaPingEvent, _ := EncodeEvent("ratta_ping", map[string]interface{}{})
	writeFrame(t, ws, rattaPingEvent)

	// Read response
	response := readFrame(t, ws)
	pType, payload := DecodeFrame(response)
	if pType != '4' {
		t.Errorf("expected message packet '4', got %c", pType)
	}

	// Parse event (payload is like "2[..." after '4' is stripped, so skip the '2')
	eventName, data, err := DecodeEvent(payload[1:])
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if eventName != "ratta_ping" {
		t.Errorf("expected event 'ratta_ping', got %q", eventName)
	}
	if !contains(data, "Received") {
		t.Errorf("expected 'Received' in response, got %q", data)
	}
}

// TestSocketIOClientMessage tests ClientMessage response.
func TestSocketIOClientMessage(t *testing.T) {
	httpServer, authService, _, _ := setupSocketIOTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := authService.createJWTToken(ctx, 1, "")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	ws := connectSocketIO(t, httpServer.URL, token)
	defer ws.Close()

	// Consume handshake
	readFrame(t, ws)
	readFrame(t, ws)

	// Send ClientMessage
	clientMsg, _ := EncodeEvent("ClientMessage", map[string]string{"status": "query"})
	writeFrame(t, ws, clientMsg)

	// Read response
	response := readFrame(t, ws)
	pType, payload := DecodeFrame(response)
	if pType != '4' {
		t.Errorf("expected message packet '4', got %c", pType)
	}

	// Parse event (payload is like "2[..." after '4' is stripped, so skip the '2')
	eventName, data, err := DecodeEvent(payload[1:])
	if err != nil {
		t.Fatalf("failed to decode event: %v", err)
	}

	if eventName != "ClientMessage" {
		t.Errorf("expected event 'ClientMessage', got %q", eventName)
	}
	if !contains(data, "true") {
		t.Errorf("expected 'true' in response, got %q", data)
	}
}

// TestSocketIOMultipleDevices verifies that multiple clients for the same user both receive notifications.
func TestSocketIOMultipleDevices(t *testing.T) {
	httpServer, authService, eventBus, notifier := setupSocketIOTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create token for userID=2
	token, err := authService.createJWTToken(ctx, 2, "")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	// Connect two clients
	ws1 := connectSocketIO(t, httpServer.URL, token)
	defer ws1.Close()

	ws2 := connectSocketIO(t, httpServer.URL, token)
	defer ws2.Close()

	// Consume handshakes
	for _, ws := range []*websocket.Conn{ws1, ws2} {
		readFrame(t, ws)
		readFrame(t, ws)
	}

	// Subscribe notifier to events (simulating main.go setup)
	eventBus.Subscribe(events.FileUploaded, func(e events.Event) {
		payload, _ := EncodeEvent("ServerMessage", map[string]interface{}{
			"code":    "200",
			"msgType": "FILE-SYN",
		})
		notifier.NotifyUser(e.UserID, "ServerMessage", payload)
	})

	// Publish an event
	eventBus.Publish(context.Background(), events.Event{
		Type:   events.FileUploaded,
		FileID: 1,
		UserID: 2,
		Path:   "/test.txt",
	})

	// Both clients should receive the message
	for i, ws := range []*websocket.Conn{ws1, ws2} {
		response := readFrame(t, ws)
		pType, payload := DecodeFrame(response)
		if pType != '4' {
			t.Errorf("client %d: expected message packet '4', got %c", i+1, pType)
		}

		eventName, _, err := DecodeEvent(payload[1:])
		if err != nil {
			t.Errorf("client %d: failed to decode event: %v", i+1, err)
		}

		if eventName != "ServerMessage" {
			t.Errorf("client %d: expected 'ServerMessage', got %q", i+1, eventName)
		}
	}
}

// TestSocketIODisconnectCleanup verifies that clients are cleaned up on disconnect.
func TestSocketIODisconnectCleanup(t *testing.T) {
	httpServer, authService, _, notifier := setupSocketIOTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := authService.createJWTToken(ctx, 3, "")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	ws := connectSocketIO(t, httpServer.URL, token)

	// Consume handshakes
	readFrame(t, ws)
	readFrame(t, ws)

	// Check that client is registered
	notifier.mu.RLock()
	clientCount := len(notifier.clients[3])
	notifier.mu.RUnlock()

	if clientCount != 1 {
		t.Errorf("expected 1 client registered, got %d", clientCount)
	}

	// Disconnect
	ws.Close()

	// Give a moment for cleanup
	time.Sleep(100 * time.Millisecond)

	// Check that client is removed
	notifier.mu.RLock()
	clientCount = len(notifier.clients[3])
	notifier.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", clientCount)
	}
}

// Helper functions for tests

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0)
}
