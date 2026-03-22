package sync

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	jsonSuccess(w, nil)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", ct)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	cd, ok := result["cd"].(string)
	if !ok || cd != "000" {
		t.Errorf("expected cd: '000', got %v", result["cd"])
	}
}

func TestJSONSuccessWithExtra(t *testing.T) {
	w := httptest.NewRecorder()
	extra := map[string]interface{}{
		"user_id":   12345,
		"username":  "testuser",
		"activated": true,
	}
	jsonSuccess(w, extra)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify success code
	if cd, ok := result["cd"].(string); !ok || cd != "000" {
		t.Errorf("expected cd: '000', got %v", result["cd"])
	}

	// Verify extra fields
	if uid, ok := result["user_id"].(float64); !ok || uid != 12345 {
		t.Errorf("expected user_id: 12345, got %v", result["user_id"])
	}
	if username, ok := result["username"].(string); !ok || username != "testuser" {
		t.Errorf("expected username: testuser, got %v", result["username"])
	}
	if activated, ok := result["activated"].(bool); !ok || !activated {
		t.Errorf("expected activated: true, got %v", result["activated"])
	}
}

func TestJSONError(t *testing.T) {
	tests := []struct {
		name       string
		err        *SyncError
		wantStatus int
		wantCode   string
	}{
		{
			name:       "wrong password",
			err:        ErrWrongPassword(),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "E0019",
		},
		{
			name:       "account locked",
			err:        ErrAccountLocked(),
			wantStatus: http.StatusForbidden,
			wantCode:   "E0045",
		},
		{
			name:       "invalid token",
			err:        ErrInvalidToken(),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "E0712",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			jsonError(w, tt.err)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, w.Code)
			}

			if ct := w.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("expected Content-Type application/json, got %s", ct)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if cd, ok := result["cd"].(string); !ok || cd != tt.wantCode {
				t.Errorf("expected cd: %s, got %v", tt.wantCode, result["cd"])
			}

			if msg, ok := result["msg"].(string); !ok || msg == "" {
				t.Errorf("expected non-empty msg, got %v", result["msg"])
			}
		})
	}
}

func TestParseJSONBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid json",
			body:    `{"key": "value", "number": 42}`,
			wantErr: false,
		},
		{
			name:    "empty object",
			body:    `{}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			body:    `{invalid}`,
			wantErr: true,
		},
		{
			name:    "non-object json",
			body:    `["array"]`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.body))
			defer req.Body.Close()

			result, err := parseJSONBody(req)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONBody error: got %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && result == nil {
				t.Errorf("expected non-nil result on success")
			}
		})
	}
}

func TestParseJSONBodyPreservesNumbers(t *testing.T) {
	body := `{"int_val": 42, "float_val": 3.14, "str_num": "12345"}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	defer req.Body.Close()

	result, err := parseJSONBody(req)
	if err != nil {
		t.Fatalf("parseJSONBody failed: %v", err)
	}

	if result == nil {
		t.Fatalf("parseJSONBody returned nil")
	}

	// With UseNumber(), numbers are json.Number strings
	// Check that we can extract them
	if intVal, ok := result["int_val"].(json.Number); !ok {
		t.Errorf("expected json.Number for int_val, got %T", result["int_val"])
	} else if intVal.String() != "42" {
		t.Errorf("expected int_val '42', got %s", intVal)
	}
}

func TestBodyStr(t *testing.T) {
	tests := []struct {
		name  string
		m     map[string]interface{}
		key   string
		want  string
	}{
		{
			name: "simple string",
			m:    map[string]interface{}{"username": "alice"},
			key:  "username",
			want: "alice",
		},
		{
			name: "missing key",
			m:    map[string]interface{}{},
			key:  "missing",
			want: "",
		},
		{
			name: "wrong type",
			m:    map[string]interface{}{"count": 42},
			key:  "count",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bodyStr(tt.m, tt.key)
			if result != tt.want {
				t.Errorf("bodyStr got %q, want %q", result, tt.want)
			}
		})
	}
}

func TestBodyInt(t *testing.T) {
	tests := []struct {
		name  string
		m     map[string]interface{}
		key   string
		want  int64
	}{
		{
			name: "simple int",
			m:    map[string]interface{}{"count": json.Number("42")},
			key:  "count",
			want: 42,
		},
		{
			name: "missing key",
			m:    map[string]interface{}{},
			key:  "missing",
			want: 0,
		},
		{
			name: "wrong type string",
			m:    map[string]interface{}{"value": "not a number"},
			key:  "value",
			want: 0,
		},
		{
			name: "float64",
			m:    map[string]interface{}{"value": float64(42)},
			key:  "value",
			want: 42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bodyInt(tt.m, tt.key)
			if result != tt.want {
				t.Errorf("bodyInt got %d, want %d", result, tt.want)
			}
		})
	}
}

func TestBodyBool(t *testing.T) {
	tests := []struct {
		name  string
		m     map[string]interface{}
		key   string
		want  bool
	}{
		{
			name: "json bool true",
			m:    map[string]interface{}{"enabled": true},
			key:  "enabled",
			want: true,
		},
		{
			name: "json bool false",
			m:    map[string]interface{}{"enabled": false},
			key:  "enabled",
			want: false,
		},
		{
			name: "text Y",
			m:    map[string]interface{}{"enabled": "Y"},
			key:  "enabled",
			want: true,
		},
		{
			name: "text N",
			m:    map[string]interface{}{"enabled": "N"},
			key:  "enabled",
			want: false,
		},
		{
			name: "text yes",
			m:    map[string]interface{}{"enabled": "yes"},
			key:  "enabled",
			want: true,
		},
		{
			name: "text no",
			m:    map[string]interface{}{"enabled": "no"},
			key:  "enabled",
			want: false,
		},
		{
			name: "missing key",
			m:    map[string]interface{}{},
			key:  "missing",
			want: false,
		},
		{
			name: "wrong type number",
			m:    map[string]interface{}{"enabled": 1},
			key:  "enabled",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bodyBool(tt.m, tt.key)
			if result != tt.want {
				t.Errorf("bodyBool got %v, want %v", result, tt.want)
			}
		})
	}
}

func TestIntegrationJSONResponseCycle(t *testing.T) {
	// Test a complete cycle: parse request -> format response
	body := `{"username": "alice", "attempt": 2}`
	req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(body)))
	defer req.Body.Close()

	parsed, err := parseJSONBody(req)
	if err != nil {
		t.Fatalf("parseJSONBody failed: %v", err)
	}

	username := bodyStr(parsed, "username")
	if username != "alice" {
		t.Errorf("expected username alice, got %s", username)
	}

	// Send success response with extra fields
	w := httptest.NewRecorder()
	extra := map[string]interface{}{
		"username": username,
		"session":  "xyz123",
	}
	jsonSuccess(w, extra)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["username"] != "alice" {
		t.Errorf("expected username in response, got %v", result)
	}
}
