package sync

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Engine.IO v3 packet types
const (
	PacketOpen    = '0' // server → client, JSON payload with session info
	PacketPing    = '2' // server → client (or bidirectional)
	PacketPong    = '3' // client → server (response to ping)
	PacketMessage = '4' // Socket.IO layer prefix
)

// Socket.IO message sub-types (after the '4' prefix)
const (
	MessageConnect = "40" // connection acknowledgment
	MessageEvent   = "42" // event frame: `42["eventName", arg1, ...]`
)

// EncodeOpenPacket returns the Engine.IO open packet with session info.
// Format: 0{"sid":"<sid>","upgrades":[],"pingInterval":<ms>,"pingTimeout":<ms>}
func EncodeOpenPacket(sid string, pingInterval, pingTimeout int) string {
	payload := map[string]interface{}{
		"sid":          sid,
		"upgrades":     []string{},
		"pingInterval": pingInterval,
		"pingTimeout":  pingTimeout,
	}
	jsonBytes, _ := json.Marshal(payload)
	return "0" + string(jsonBytes)
}

// EncodeEvent returns a Socket.IO event frame.
// Format: 42["<eventName>",<jsonData>]
func EncodeEvent(eventName string, data any) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal event data: %w", err)
	}
	// Build the array format: ["eventName", data]
	return fmt.Sprintf(`42["%s",%s]`, eventName, string(jsonData)), nil
}

// DecodeFrame splits the packet type byte from the payload.
// Returns first byte as type, rest as payload.
func DecodeFrame(raw string) (packetType byte, payload string) {
	if len(raw) == 0 {
		return 0, ""
	}
	return raw[0], raw[1:]
}

// DecodeEvent extracts the event name and data from a Socket.IO event frame.
// Expects format after "42" prefix: `["eventName", ...]`
// Returns event name and the raw JSON of the remaining args.
func DecodeEvent(payload string) (eventName string, data string, err error) {
	// payload should be like: ["eventName", {...}, ...]
	if !strings.HasPrefix(payload, "[") || !strings.HasSuffix(payload, "]") {
		return "", "", fmt.Errorf("invalid event frame format: %s", payload)
	}

	// Trim brackets
	inner := payload[1 : len(payload)-1]

	// Simple parsing: find first string (eventName), then extract remaining data
	// For now, use a simple state machine to find the event name
	var inString bool
	var escaped bool
	var eventNameEnd int

	for i := 0; i < len(inner); i++ {
		c := inner[i]

		if escaped {
			escaped = false
			continue
		}

		if c == '\\' {
			escaped = true
			continue
		}

		if c == '"' {
			if !inString {
				// Start of string
				inString = true
			} else {
				// End of string
				eventNameEnd = i
				inString = false
				break
			}
		}
	}

	if eventNameEnd == 0 {
		return "", "", fmt.Errorf("could not find event name in frame: %s", payload)
	}

	// Extract the event name (skip the opening quote)
	eventName = inner[1:eventNameEnd]

	// Extract remaining data (after the comma and space, if present)
	// Format: "eventName", ... or "eventName", {...}
	remaining := strings.TrimSpace(inner[eventNameEnd+1:])
	if strings.HasPrefix(remaining, ",") {
		remaining = strings.TrimSpace(remaining[1:])
	}

	return eventName, remaining, nil
}
