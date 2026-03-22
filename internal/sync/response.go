package sync

import (
	"encoding/json"
	"net/http"
	"strings"
)

// jsonSuccess writes a successful SPC response with status 200 and code "000".
// Extra fields (if provided) are merged into the response body.
func jsonSuccess(w http.ResponseWriter, extra map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]interface{}{
		"cd": "000",
	}

	// Merge extra fields
	if extra != nil {
		for k, v := range extra {
			response[k] = v
		}
	}

	json.NewEncoder(w).Encode(response)
}

// jsonError writes an error response with the given SyncError.
// Response contains error code and message, with HTTP status from the error.
func jsonError(w http.ResponseWriter, err *SyncError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.HTTPStatus)

	response := map[string]interface{}{
		"cd":  err.Code,
		"msg": err.Message,
	}

	json.NewEncoder(w).Encode(response)
}

// parseJSONBody decodes the request body as JSON, returning a map.
// Uses json.Decoder with UseNumber() to preserve number precision.
// Returns an error if the body is not valid JSON or not an object.
func parseJSONBody(r *http.Request) (map[string]interface{}, error) {
	defer r.Body.Close()

	var result map[string]interface{}

	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()

	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}

// bodyStr extracts a string value from a parsed JSON body.
// Returns empty string if the key is missing or not a string.
func bodyStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// bodyInt extracts an int64 value from a parsed JSON body.
// Handles json.Number (from UseNumber() decoder) and float64 types.
// Returns 0 if the key is missing or cannot be converted to int64.
func bodyInt(m map[string]interface{}, key string) int64 {
	if v, ok := m[key]; ok {
		switch x := v.(type) {
		case json.Number:
			// Parse json.Number string to int64
			val, err := x.Int64()
			if err == nil {
				return val
			}
		case float64:
			return int64(x)
		}
	}
	return 0
}

// bodyBool extracts a boolean value from a parsed JSON body.
// Handles JSON booleans, and text booleans ("Y"/"N", "yes"/"no").
// Returns false if the key is missing or cannot be converted to bool.
func bodyBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		switch x := v.(type) {
		case bool:
			return x
		case string:
			lower := strings.ToLower(x)
			return lower == "y" || lower == "yes" || lower == "true"
		}
	}
	return false
}
