package sync

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeOpenPacket(t *testing.T) {
	packet, err := EncodeOpenPacket("test-sid", 25000, 5000)
	if err != nil {
		t.Fatalf("EncodeOpenPacket failed: %v", err)
	}

	// Should start with '0'
	if packet[0] != '0' {
		t.Errorf("expected packet to start with '0', got %c", packet[0])
	}

	// Parse the JSON portion
	var payload map[string]interface{}
	err = json.Unmarshal([]byte(packet[1:]), &payload)
	if err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	// Verify fields
	if payload["sid"] != "test-sid" {
		t.Errorf("expected sid='test-sid', got %v", payload["sid"])
	}
	if payload["pingInterval"] != float64(25000) {
		t.Errorf("expected pingInterval=25000, got %v", payload["pingInterval"])
	}
	if payload["pingTimeout"] != float64(5000) {
		t.Errorf("expected pingTimeout=5000, got %v", payload["pingTimeout"])
	}
	if upgrades, ok := payload["upgrades"].([]interface{}); !ok || len(upgrades) != 0 {
		t.Errorf("expected empty upgrades array, got %v", payload["upgrades"])
	}
}

func TestEncodeEvent(t *testing.T) {
	testData := map[string]string{"key": "value"}
	event, err := EncodeEvent("TestEvent", testData)
	if err != nil {
		t.Fatalf("EncodeEvent failed: %v", err)
	}

	// Should start with '42'
	if !strings.HasPrefix(event, "42") {
		t.Errorf("expected event to start with '42', got %s", event[:2])
	}

	// Should have brackets
	if !strings.HasPrefix(event[2:], "[") || !strings.HasSuffix(event, "]") {
		t.Errorf("expected event to have array format, got %s", event)
	}
}

func TestEncodeEventServerMessage(t *testing.T) {
	payload := map[string]interface{}{
		"code":      "200",
		"timestamp": 1234567890,
		"msgType":   "FILE-SYN",
	}
	event, err := EncodeEvent("ServerMessage", payload)
	if err != nil {
		t.Fatalf("EncodeEvent failed: %v", err)
	}

	// Check format
	if !strings.HasPrefix(event, "42[\"ServerMessage\",") {
		t.Errorf("expected event to start with '42[\"ServerMessage\",', got %s", event[:30])
	}
}

func TestDecodeFrame(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedType byte
		expectedData string
	}{
		{
			name:         "ping frame",
			input:        "2",
			expectedType: '2',
			expectedData: "",
		},
		{
			name:         "pong frame",
			input:        "3",
			expectedType: '3',
			expectedData: "",
		},
		{
			name:         "message frame with payload",
			input:        `42["test", "data"]`,
			expectedType: '4',
			expectedData: `2["test", "data"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pType, payload := DecodeFrame(tt.input)
			if pType != tt.expectedType {
				t.Errorf("expected type %c, got %c", tt.expectedType, pType)
			}
			if payload != tt.expectedData {
				t.Errorf("expected payload %s, got %s", tt.expectedData, payload)
			}
		})
	}
}

func TestDecodeEvent(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedEvent string
		expectedData  string
	}{
		{
			name:          "simple event",
			input:         `["ratta_ping"]`,
			expectedEvent: "ratta_ping",
			expectedData:  "",
		},
		{
			name:          "event with string data",
			input:         `["ratta_ping","Received"]`,
			expectedEvent: "ratta_ping",
			expectedData:  `"Received"`,
		},
		{
			name:          "event with object data",
			input:         `["ClientMessage",{"status":"query"}]`,
			expectedEvent: "ClientMessage",
			expectedData:  `{"status":"query"}`,
		},
		{
			name:          "ServerMessage event",
			input:         `["ServerMessage",{"code":"200","msgType":"FILE-SYN"}]`,
			expectedEvent: "ServerMessage",
			expectedData:  `{"code":"200","msgType":"FILE-SYN"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, data, err := DecodeEvent(tt.input)
			if err != nil {
				t.Fatalf("DecodeEvent failed: %v", err)
			}
			if event != tt.expectedEvent {
				t.Errorf("expected event %q, got %q", tt.expectedEvent, event)
			}
			if data != tt.expectedData {
				t.Errorf("expected data %q, got %q", tt.expectedData, data)
			}
		})
	}
}

func TestDecodeEventInvalidFormat(t *testing.T) {
	tests := []string{
		"",
		"no brackets",
		`["incomplete`,
		"[incomplete]",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, _, err := DecodeEvent(input)
			if err == nil {
				t.Errorf("expected error for invalid input %q", input)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	// Encode an event
	originalData := map[string]string{"name": "test", "status": "ok"}
	encoded, err := EncodeEvent("TestEvent", originalData)
	if err != nil {
		t.Fatalf("EncodeEvent failed: %v", err)
	}

	// Remove the "42" prefix and decode
	payload := encoded[2:]
	eventName, decodedDataStr, err := DecodeEvent(payload)
	if err != nil {
		t.Fatalf("DecodeEvent failed: %v", err)
	}

	if eventName != "TestEvent" {
		t.Errorf("expected event name 'TestEvent', got %q", eventName)
	}

	// Parse the decoded data back to a map
	var decodedData map[string]string
	err = json.Unmarshal([]byte(decodedDataStr), &decodedData)
	if err != nil {
		t.Fatalf("failed to unmarshal decoded data: %v", err)
	}

	if decodedData["name"] != originalData["name"] || decodedData["status"] != originalData["status"] {
		t.Errorf("decoded data mismatch: expected %v, got %v", originalData, decodedData)
	}
}
