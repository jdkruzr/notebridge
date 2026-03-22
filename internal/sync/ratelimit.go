package sync

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter implements per-IP rate limiting for auth endpoints.
type RateLimiter struct {
	mu sync.Mutex

	// Per-IP tracking
	ipStates map[string]*ipState

	// Configuration
	requestsPerWindow int
	windowDuration    time.Duration
	blockDuration     time.Duration

	// Cleanup ticker
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

type ipState struct {
	attempts    int
	firstAttempt time.Time
	blockedUntil time.Time
}

// NewRateLimiter creates a new rate limiter with default settings:
// - 20 requests per 15 minutes per IP
// - 15-minute block duration when limit exceeded
// - Cleanup every 5 minutes
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		ipStates:          make(map[string]*ipState),
		requestsPerWindow: 20,
		windowDuration:    15 * time.Minute,
		blockDuration:     15 * time.Minute,
		cleanupTicker:     time.NewTicker(5 * time.Minute),
		stopCleanup:       make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupWorker()

	return rl
}

// Stop stops the rate limiter's cleanup worker.
func (rl *RateLimiter) Stop() {
	rl.cleanupTicker.Stop()
	close(rl.stopCleanup)
}

// Allow checks if the given IP is allowed to make a request.
// Returns false if the IP is rate limited.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Get or create IP state
	state, exists := rl.ipStates[ip]
	if !exists {
		state = &ipState{}
		rl.ipStates[ip] = state
	}

	// Check if currently blocked
	if !state.blockedUntil.IsZero() && now.Before(state.blockedUntil) {
		return false
	}

	// If we were blocked but block duration has expired, reset everything
	if !state.blockedUntil.IsZero() && now.After(state.blockedUntil) {
		state.attempts = 0
		state.firstAttempt = time.Time{}
		state.blockedUntil = time.Time{}
	}

	// Check if we're still in the window from the first attempt
	if !state.firstAttempt.IsZero() && now.Before(state.firstAttempt.Add(rl.windowDuration)) {
		// Still in window - increment attempts
		state.attempts++

		// Check if we've exceeded the limit
		if state.attempts > rl.requestsPerWindow {
			state.blockedUntil = now.Add(rl.blockDuration)
			return false
		}

		return true
	}

	// Window expired or no prior attempts - reset and allow
	state.attempts = 1
	state.firstAttempt = now
	state.blockedUntil = time.Time{}

	return true
}

// cleanupWorker periodically removes expired IP states to prevent memory growth.
func (rl *RateLimiter) cleanupWorker() {
	for {
		select {
		case <-rl.cleanupTicker.C:
			rl.cleanup()
		case <-rl.stopCleanup:
			return
		}
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, state := range rl.ipStates {
		// Remove entry if both conditions are met:
		// 1. Not currently blocked
		// 2. Window has expired since first attempt
		if state.blockedUntil.IsZero() || now.After(state.blockedUntil) {
			// Not blocked (or was blocked but has expired)
			// Remove if window has also expired
			if !state.firstAttempt.IsZero() && now.After(state.firstAttempt.Add(rl.windowDuration)) {
				delete(rl.ipStates, ip)
			}
		}
	}
}

// RateLimitMiddleware returns middleware that applies rate limiting based on client IP.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := extractClientIP(r)

			if !limiter.Allow(clientIP) {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the client IP from the request.
// Checks X-Forwarded-For first (for proxies), falls back to RemoteAddr.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, use the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			if ip := strings.TrimSpace(ips[0]); ip != "" {
				return ip
			}
		}
	}

	// Fall back to RemoteAddr
	if remoteAddr := r.RemoteAddr; remoteAddr != "" {
		// RemoteAddr might include port, extract just the IP
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
			return host
		}
		return remoteAddr
	}

	// Fallback
	return "unknown"
}
