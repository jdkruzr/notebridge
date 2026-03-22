package sync

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"

	"golang.org/x/net/websocket"
)

// SocketIOHandler returns a WebSocket handler for Engine.IO v3 Socket.IO connections.
// Validates JWT from query params, manages handshake, ping/pong, and message routing.
func SocketIOHandler(auth *AuthService, notifier *NotifyManager, logger *slog.Logger) websocket.Handler {
	return func(ws *websocket.Conn) {
		// Read token and type from URL query params
		token := ws.Request().URL.Query().Get("token")
		deviceType := ws.Request().URL.Query().Get("type")

		// Validate JWT
		userID, _, err := auth.ValidateJWTToken(ws.Request().Context(), token)
		if err != nil {
			logger.Warn("socket.io authentication failed", "error", err)
			// Send error frame and close
			errorFrame := `44{"message":"Authentication failed"}`
			io.WriteString(ws, errorFrame)
			ws.Close()
			return
		}

		logger.Info("socket.io client connected", "userID", userID, "deviceType", deviceType)

		// Generate random session ID (16-byte hex)
		sidBytes := make([]byte, 16)
		if _, err := rand.Read(sidBytes); err != nil {
			logger.Error("failed to generate session ID", "error", err)
			ws.Close()
			return
		}
		sid := hex.EncodeToString(sidBytes)

		// Send open packet (AC3.1)
		openPacket := EncodeOpenPacket(sid, 25000, 5000)
		if _, err := io.WriteString(ws, openPacket); err != nil {
			logger.Warn("failed to write open packet", "error", err)
			ws.Close()
			return
		}

		// Send connect ack
		if _, err := io.WriteString(ws, MessageConnect); err != nil {
			logger.Warn("failed to write connect ack", "error", err)
			ws.Close()
			return
		}

		// Create client and register with notifier
		client := &wsClient{
			userID:     userID,
			deviceType: deviceType,
			send:       make(chan string, 16),
			done:       make(chan struct{}),
		}
		notifier.Register(client)

		// Start write goroutine
		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("socket.io write goroutine panic", "panic", r)
				}
			}()

			for {
				select {
				case msg, ok := <-client.send:
					if !ok {
						return
					}
					if _, err := io.WriteString(ws, msg); err != nil {
						logger.Debug("failed to write to websocket", "error", err)
						return
					}
				case <-client.done:
					return
				}
			}
		}()

		// Read loop
		defer func() {
			notifier.Unregister(client)
			ws.Close()
			logger.Info("socket.io client disconnected", "userID", userID)
		}()

		for {
			var rawFrame string
			if err := websocket.Message.Receive(ws, &rawFrame); err != nil {
				if err == io.EOF {
					return
				}
				logger.Debug("websocket read error", "error", err)
				return
			}

			// Decode frame
			packetType, payload := DecodeFrame(rawFrame)

			switch packetType {
			case PacketPing:
				// Respond with pong (AC3.2)
				if _, err := io.WriteString(ws, string(PacketPong)); err != nil {
					logger.Debug("failed to write pong", "error", err)
					return
				}

			case PacketMessage:
				// Handle Socket.IO message
				// payload here is like "2[...] after the '4' is stripped
				if !readyForSocketIOMessage(payload) {
					continue
				}

				eventName, data, err := DecodeEvent(payload[1:]) // Strip "2" prefix (Socket.IO layer)
				if err != nil {
					logger.Debug("failed to decode event", "error", err)
					continue
				}

				switch eventName {
				case "ratta_ping":
					// Respond with ratta_ping acknowledgment
					response, _ := EncodeEvent("ratta_ping", "Received")
					if _, err := io.WriteString(ws, response); err != nil {
						logger.Debug("failed to write ratta_ping response", "error", err)
						return
					}

				case "ClientMessage":
					// Respond with true status
					response, _ := EncodeEvent("ClientMessage", "true")
					if _, err := io.WriteString(ws, response); err != nil {
						logger.Debug("failed to write ClientMessage response", "error", err)
						return
					}

				default:
					logger.Debug("unknown socket.io event", "event", eventName, "data", data)
				}

			default:
				logger.Debug("unknown packet type", "type", string(packetType))
			}
		}
	}
}

// readyForSocketIOMessage checks if the payload starts with the Socket.IO message prefix.
// Since the '4' prefix is already stripped by DecodeFrame, we check for '2' (which is part of "42").
func readyForSocketIOMessage(payload string) bool {
	return len(payload) >= 1 && payload[0] == '2'
}
