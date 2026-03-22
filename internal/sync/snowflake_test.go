package sync

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestSnowflakeIDsAreUnique(t *testing.T) {
	t.Helper()
	gen := NewSnowflakeGenerator()

	const count = 1000
	ids := make(map[int64]bool)

	for i := 0; i < count; i++ {
		id := gen.Generate()
		if ids[id] {
			t.Fatalf("duplicate ID generated: %d", id)
		}
		ids[id] = true
	}

	if len(ids) != count {
		t.Errorf("expected %d unique IDs, got %d", count, len(ids))
	}
}

func TestSnowflakeIDsAreMonotonicallyIncreasing(t *testing.T) {
	t.Helper()
	gen := NewSnowflakeGenerator()

	const count = 100
	var prevID int64

	for i := 0; i < count; i++ {
		id := gen.Generate()
		if id <= prevID {
			t.Errorf("ID not monotonically increasing: %d <= %d", id, prevID)
		}
		prevID = id
	}
}

func TestSnowflakeIDJSONMarshal(t *testing.T) {
	t.Helper()

	tests := []struct {
		name     string
		id       SnowflakeID
		wantJSON string
	}{
		{
			name:     "simple ID",
			id:       SnowflakeID(12345),
			wantJSON: `"12345"`,
		},
		{
			name:     "zero",
			id:       SnowflakeID(0),
			wantJSON: `"0"`,
		},
		{
			name:     "large ID",
			id:       SnowflakeID(9223372036854775807), // max int64
			wantJSON: `"9223372036854775807"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(tt.id)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}

			if string(b) != tt.wantJSON {
				t.Errorf("got %s, want %s", string(b), tt.wantJSON)
			}
		})
	}
}

func TestSnowflakeIDJSONUnmarshal(t *testing.T) {
	t.Helper()

	tests := []struct {
		name      string
		jsonBytes []byte
		want      SnowflakeID
		wantErr   bool
	}{
		{
			name:      "string ID",
			jsonBytes: []byte(`"12345"`),
			want:      SnowflakeID(12345),
			wantErr:   false,
		},
		{
			name:      "numeric ID",
			jsonBytes: []byte(`12345`),
			want:      SnowflakeID(12345),
			wantErr:   false,
		},
		{
			name:      "zero",
			jsonBytes: []byte(`"0"`),
			want:      SnowflakeID(0),
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var id SnowflakeID
			err := json.Unmarshal(tt.jsonBytes, &id)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unmarshal error: got %v, wantErr %v", err, tt.wantErr)
			}
			if id != tt.want {
				t.Errorf("got %d, want %d", id, tt.want)
			}
		})
	}
}

func TestSnowflakeIDRoundtrip(t *testing.T) {
	t.Helper()
	gen := NewSnowflakeGenerator()

	for i := 0; i < 10; i++ {
		original := SnowflakeID(gen.Generate())

		// Marshal to JSON
		b, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		// Unmarshal back
		var unmarshaled SnowflakeID
		err = json.Unmarshal(b, &unmarshaled)
		if err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}

		if unmarshaled != original {
			t.Errorf("roundtrip failed: %d != %d", unmarshaled, original)
		}
	}
}

func TestSnowflakeIDConcurrentSafety(t *testing.T) {
	t.Helper()
	gen := NewSnowflakeGenerator()

	const goroutines = 10
	const idsPerGoroutine = 100

	ids := make(map[int64]bool)
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < idsPerGoroutine; i++ {
				id := gen.Generate()
				mu.Lock()
				ids[id] = true
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	expectedCount := goroutines * idsPerGoroutine
	if len(ids) != expectedCount {
		t.Errorf("expected %d unique IDs from %d goroutines, got %d",
			expectedCount, goroutines, len(ids))
	}
}
