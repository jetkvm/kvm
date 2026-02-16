package kvm

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Rate limiting configuration
const (
	maxAttempts     = 5                // Maximum attempts before lockout
	baseWindow      = 15 * time.Minute // Initial lockout window
	maxBackoffLevel = 3                // Maximum backoff multiplier (15min, 30min, 1hr)
	cleanupInterval = 5 * time.Minute  // How often to clean stale entries
)

// rateLimitEntry tracks failed attempts for a single IP
type rateLimitEntry struct {
	attempts     int       // Number of failed attempts in current window
	windowStart  time.Time // When the current window started
	backoffLevel int       // Exponential backoff level (0 = base, 1 = 2x, 2 = 4x, etc.)
	lockedUntil  time.Time // When the lockout expires (if locked)
}

// RateLimiter provides IP-based rate limiting for password-related endpoints
type RateLimiter struct {
	mu      sync.RWMutex
	entries map[string]*rateLimitEntry
	stopCh  chan struct{}
}

// NewRateLimiter creates a new rate limiter with automatic cleanup
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		stopCh:  make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// cleanupLoop periodically removes stale entries to prevent memory growth
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.cleanup()
		case <-rl.stopCh:
			return
		}
	}
}

// cleanup removes entries that have expired
func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, entry := range rl.entries {
		// Remove if not locked and window has expired
		windowDuration := rl.getWindowDuration(entry.backoffLevel)
		if now.After(entry.lockedUntil) && now.Sub(entry.windowStart) > windowDuration {
			delete(rl.entries, ip)
		}
	}
}

// getWindowDuration calculates the window duration based on backoff level
func (rl *RateLimiter) getWindowDuration(backoffLevel int) time.Duration {
	if backoffLevel > maxBackoffLevel {
		backoffLevel = maxBackoffLevel
	}
	// Exponential backoff: 15min, 30min, 1hr, 2hr
	multiplier := 1 << backoffLevel // 1, 2, 4, 8
	return baseWindow * time.Duration(multiplier)
}

// IsAllowed checks if the IP is allowed to make an attempt.
// Returns (allowed, retryAfterSeconds)
func (rl *RateLimiter) IsAllowed(ip string) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[ip]

	if !exists {
		return true, 0
	}

	// Check if currently locked out
	if now.Before(entry.lockedUntil) {
		retryAfter := int(entry.lockedUntil.Sub(now).Seconds()) + 1
		return false, retryAfter
	}

	// Check if window has expired (reset attempts)
	windowDuration := rl.getWindowDuration(entry.backoffLevel)
	if now.Sub(entry.windowStart) > windowDuration {
		// Window expired, allow the attempt (will be tracked on failure)
		return true, 0
	}

	// Check if under the limit
	if entry.attempts < maxAttempts {
		return true, 0
	}

	// At limit but not locked - shouldn't happen, but handle gracefully
	return true, 0
}

// RecordFailure records a failed authentication attempt for the IP
func (rl *RateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.entries[ip]

	if !exists {
		rl.entries[ip] = &rateLimitEntry{
			attempts:     1,
			windowStart:  now,
			backoffLevel: 0,
		}
		return
	}

	// Check if window has expired
	windowDuration := rl.getWindowDuration(entry.backoffLevel)
	if now.Sub(entry.windowStart) > windowDuration {
		// Start new window, but increase backoff if they were previously locked
		if entry.backoffLevel > 0 || entry.attempts >= maxAttempts {
			entry.backoffLevel++
			if entry.backoffLevel > maxBackoffLevel {
				entry.backoffLevel = maxBackoffLevel
			}
		}
		entry.attempts = 1
		entry.windowStart = now
		entry.lockedUntil = time.Time{}
		return
	}

	entry.attempts++

	// Lock out if exceeded attempts
	if entry.attempts >= maxAttempts {
		lockDuration := rl.getWindowDuration(entry.backoffLevel)
		entry.lockedUntil = now.Add(lockDuration)
	}
}

// RecordSuccess clears the rate limit entry for an IP after successful auth
func (rl *RateLimiter) RecordSuccess(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.entries, ip)
}

// Stop stops the cleanup goroutine
func (rl *RateLimiter) Stop() {
	close(rl.stopCh)
}

// Global rate limiter instance for password endpoints
var passwordRateLimiter = NewRateLimiter()

// RateLimitMiddleware creates a Gin middleware that applies rate limiting
func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		allowed, retryAfter := passwordRateLimiter.IsAllowed(ip)
		if !allowed {
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Too many failed attempts. Please try again later.",
				"retry_after": retryAfter,
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
