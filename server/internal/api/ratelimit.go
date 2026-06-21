// Package api — per-IP token-bucket rate limiter middleware.
package api

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter is a per-IP token-bucket rate limiter. Safe for concurrent use.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64 // tokens per second
	burst    int     // max token burst
	cleanup  time.Time
}

type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}

// NewRateLimiter creates a limiter allowing `rate` requests per second with
// the given burst capacity.
func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	return &RateLimiter{
		buckets: map[string]*tokenBucket{},
		rate:    ratePerSec,
		burst:   burst,
		cleanup: time.Now(),
	}
}

// Allow reports whether a request from the given key should be allowed.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Periodic cleanup: drop buckets idle for > 5 minutes.
	if now.Sub(rl.cleanup) > 5*time.Minute {
		for k, b := range rl.buckets {
			if now.Sub(b.lastFill) > 5*time.Minute {
				delete(rl.buckets, k)
			}
		}
		rl.cleanup = now
	}

	b, ok := rl.buckets[key]
	if !ok {
		b = &tokenBucket{tokens: float64(rl.burst), lastFill: now}
		rl.buckets[key] = b
	}
	// Refill tokens.
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastFill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// Middleware returns an HTTP middleware that rate-limits per IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		// If behind a proxy, trust X-Forwarded-For.
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = fwd
		}
		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MiddlewareFunc returns afunc that rate-limits per IP. Shorthand for simple routes.
func (rl *RateLimiter) MiddlewareFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			ip = fwd
		}
		if !rl.Allow(ip) {
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
