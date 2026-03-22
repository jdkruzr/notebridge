package sync

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter_UnderLimitAllowsRequests(t *testing.T) {
	rl := NewRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.1"

	// Make 20 requests - should all succeed
	for i := 0; i < 20; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d should be allowed, but was blocked", i+1)
		}
	}
}

func TestRateLimiter_ExceedLimitBlocksIP(t *testing.T) {
	rl := NewRateLimiter()
	defer rl.Stop()

	ip := "192.168.1.1"

	// Make 20 requests - should all succeed
	for i := 0; i < 20; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d should be allowed, but was blocked", i+1)
		}
	}

	// 21st request should fail
	if rl.Allow(ip) {
		t.Fatal("21st request should be blocked, but was allowed")
	}

	// Further requests should also fail
	if rl.Allow(ip) {
		t.Fatal("22nd request should be blocked, but was allowed")
	}
}

func TestRateLimiter_DifferentIPsTrackedIndependently(t *testing.T) {
	rl := NewRateLimiter()
	defer rl.Stop()

	ip1 := "192.168.1.1"
	ip2 := "192.168.1.2"

	// Max out IP1
	for i := 0; i < 20; i++ {
		if !rl.Allow(ip1) {
			t.Fatalf("IP1 request %d should be allowed", i+1)
		}
	}

	// IP1 should now be blocked
	if rl.Allow(ip1) {
		t.Fatal("IP1 should be blocked after 20 requests")
	}

	// IP2 should still work
	for i := 0; i < 20; i++ {
		if !rl.Allow(ip2) {
			t.Fatalf("IP2 request %d should be allowed", i+1)
		}
	}

	// IP2 should now also be blocked
	if rl.Allow(ip2) {
		t.Fatal("IP2 should be blocked after 20 requests")
	}
}

func TestRateLimiter_WindowExpiryResetsCount(t *testing.T) {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 3,
		windowDuration:    100 * time.Millisecond,
		blockDuration:     100 * time.Millisecond,
		cleanupTicker:     time.NewTicker(1 * time.Second),
		stopCleanup:       make(chan struct{}),
	}
	defer rl.Stop()

	ip := "192.168.1.1"

	// Make 3 requests within the window
	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// 4th request should fail
	if rl.Allow(ip) {
		t.Fatal("4th request should be blocked")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should now allow requests again
	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d after window expiry should be allowed", i+1)
		}
	}
}

func TestRateLimiter_BlockDurationEnforced(t *testing.T) {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 2,
		windowDuration:    100 * time.Millisecond,
		blockDuration:     200 * time.Millisecond,
		cleanupTicker:     time.NewTicker(1 * time.Second),
		stopCleanup:       make(chan struct{}),
	}
	defer rl.Stop()

	ip := "192.168.1.1"

	// Fill the window
	for i := 0; i < 2; i++ {
		if !rl.Allow(ip) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	// Trigger block
	if rl.Allow(ip) {
		t.Fatal("3rd request should trigger block")
	}

	// Wait for window to expire but stay within block duration
	time.Sleep(150 * time.Millisecond)

	// Should still be blocked (window expired but block duration active)
	if rl.Allow(ip) {
		t.Fatal("should still be blocked during block duration")
	}

	// Wait for block to fully expire
	time.Sleep(100 * time.Millisecond)

	// Should now allow requests
	if !rl.Allow(ip) {
		t.Fatal("should be allowed after block duration expires")
	}
}

func TestRateLimitMiddleware_AllowedRequest(t *testing.T) {
	rl := NewRateLimiter()
	defer rl.Stop()

	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	if w.Body.String() != "OK" {
		t.Fatalf("expected 'OK', got %q", w.Body.String())
	}
}

func TestRateLimitMiddleware_BlockedRequest(t *testing.T) {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 1,
		windowDuration:    1 * time.Minute,
		blockDuration:     1 * time.Minute,
		cleanupTicker:     time.NewTicker(5 * time.Minute),
		stopCleanup:       make(chan struct{}),
	}
	defer rl.Stop()

	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// First request should succeed
	req1 := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req1.RemoteAddr = "192.168.1.1:12345"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request should be blocked
	req2 := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req2.RemoteAddr = "192.168.1.1:12345"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}
}

func TestRateLimitMiddleware_XForwardedForHeader(t *testing.T) {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 1,
		windowDuration:    1 * time.Minute,
		blockDuration:     1 * time.Minute,
		cleanupTicker:     time.NewTicker(5 * time.Minute),
		stopCleanup:       make(chan struct{}),
	}
	defer rl.Stop()

	handler := RateLimitMiddleware(rl)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request with X-Forwarded-For should succeed
	req1 := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req1.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", w1.Code)
	}

	// Second request with same X-Forwarded-For should be blocked
	req2 := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req2.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d", w2.Code)
	}

	// Request from different IP should succeed
	req3 := httptest.NewRequest("POST", "/api/user/login/challenge", nil)
	req3.Header.Set("X-Forwarded-For", "203.0.113.2")
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Fatalf("third request from different IP: expected 200, got %d", w3.Code)
	}
}

func TestExtractClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	ip := extractClientIP(req)
	if ip != "192.168.1.1" {
		t.Fatalf("expected 192.168.1.1, got %q", ip)
	}
}

func TestExtractClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 198.51.100.1")

	ip := extractClientIP(req)
	if ip != "203.0.113.1" {
		t.Fatalf("expected 203.0.113.1, got %q", ip)
	}
}

func TestExtractClientIP_XForwardedForWithSpaces(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "  203.0.113.1  ,  198.51.100.1  ")

	ip := extractClientIP(req)
	if ip != "203.0.113.1" {
		t.Fatalf("expected 203.0.113.1, got %q", ip)
	}
}

func TestRateLimiter_CleanupRemovesExpiredEntries(t *testing.T) {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 1,
		windowDuration:    50 * time.Millisecond,
		blockDuration:     50 * time.Millisecond,
		cleanupTicker:     time.NewTicker(1 * time.Second), // Don't run during test
		stopCleanup:       make(chan struct{}),
	}
	defer rl.Stop()

	ip := "192.168.1.1"

	// Generate some state by hitting the rate limit
	if !rl.Allow(ip) {
		t.Fatal("first request should succeed")
	}
	if rl.Allow(ip) {
		t.Fatal("second request should be blocked")
	}

	// Verify state exists and is blocked
	rl.mu.Lock()
	state, exists := rl.ipStates[ip]
	if !exists {
		t.Fatal("IP state should exist")
	}
	if state.blockedUntil.IsZero() {
		t.Fatal("IP should be blocked")
	}
	rl.mu.Unlock()

	// Wait for both window and block to expire
	time.Sleep(150 * time.Millisecond)

	// Call cleanup directly
	rl.cleanup()

	// Cleanup should have removed the entry
	rl.mu.Lock()
	if _, exists := rl.ipStates[ip]; exists {
		t.Fatal("IP state should have been cleaned up")
	}
	rl.mu.Unlock()
}
