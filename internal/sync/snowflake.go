package sync

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// Epoch is the timestamp in milliseconds at which the snowflake IDs started (2020-01-01T00:00:00Z)
const Epoch = 1577836800000

// SnowflakeGenerator generates time-ordered 64-bit IDs.
// Layout: 1 unused | 41 bits timestamp | 10 bits worker | 12 bits sequence
type SnowflakeGenerator struct {
	mu       sync.Mutex
	lastTime int64
	sequence int64
	workerID int64
}

// NewSnowflakeGenerator creates a new Snowflake ID generator.
// Uses workerID=1 (hardcoded for single-instance deployments).
func NewSnowflakeGenerator() *SnowflakeGenerator {
	return &SnowflakeGenerator{
		workerID: 1,
		lastTime: time.Now().UnixMilli() - Epoch,
		sequence: 0,
	}
}

// Generate generates a new unique Snowflake ID.
func (s *SnowflakeGenerator) Generate() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli() - Epoch

	// If time has advanced, reset sequence counter
	if now > s.lastTime {
		s.lastTime = now
		s.sequence = 0
	} else if now == s.lastTime {
		// Time hasn't advanced, increment sequence
		s.sequence++
		if s.sequence >= (1 << 12) { // Overflow check (2^12 = 4096)
			// Wait for next millisecond
			for now == s.lastTime {
				time.Sleep(1 * time.Millisecond)
				now = time.Now().UnixMilli() - Epoch
			}
			s.lastTime = now
			s.sequence = 0
		}
	} else {
		// Clock went backwards - this shouldn't happen in normal operation
		// but we handle it by resetting
		s.lastTime = now
		s.sequence = 0
	}

	// Build ID: 1 unused | 41 bits timestamp | 10 bits worker | 12 bits sequence
	id := (now << 22) | (s.workerID << 12) | s.sequence
	return id
}

// SnowflakeID represents a Snowflake ID that marshals to/from JSON as a string.
type SnowflakeID int64

// MarshalJSON marshals SnowflakeID as a JSON string.
func (id SnowflakeID) MarshalJSON() ([]byte, error) {
	return json.Marshal(strconv.FormatInt(int64(id), 10))
}

// UnmarshalJSON unmarshals SnowflakeID from a JSON string or number.
func (id *SnowflakeID) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	var num int64
	switch x := v.(type) {
	case float64:
		num = int64(x)
	case string:
		var err error
		num, err = strconv.ParseInt(x, 10, 64)
		if err != nil {
			return err
		}
	}

	*id = SnowflakeID(num)
	return nil
}
