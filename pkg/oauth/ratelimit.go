package oauth

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultOAuthTokenRateLimit    = 120
	defaultOAuthRegisterRateLimit = 30
	defaultOAuthRateLimitWindow   = time.Minute
)

type memoryRateLimiter struct {
	limit  int
	window time.Duration
	inner  *oauthRateLimiter
}

func newMemoryRateLimiter(limit int, window time.Duration) *memoryRateLimiter {
	return &memoryRateLimiter{
		limit:  limit,
		window: window,
		inner:  newOAuthRateLimiter(limit, window),
	}
}

func (l *memoryRateLimiter) Allow(_ context.Context, _, bucketKey string, limit int, window time.Duration, _ time.Time) (bool, error) {
	if l == nil || l.inner == nil {
		return true, nil
	}
	if limit <= 0 {
		limit = l.limit
	}
	if window <= 0 {
		window = l.window
	}
	if limit != l.limit || window != l.window {
		l.inner = newOAuthRateLimiter(limit, window)
		l.limit = limit
		l.window = window
	}
	return l.inner.allow(bucketKey), nil
}

type oauthRateLimiter struct {
	mu      sync.Mutex
	entries map[string]rateLimitEntry
	limit   int
	window  time.Duration
}

type rateLimitEntry struct {
	windowStart time.Time
	count       int
}

func newOAuthRateLimiter(limit int, window time.Duration) *oauthRateLimiter {
	if limit <= 0 {
		limit = defaultOAuthTokenRateLimit
	}
	if window <= 0 {
		window = defaultOAuthRateLimitWindow
	}
	return &oauthRateLimiter{
		entries: make(map[string]rateLimitEntry),
		limit:   limit,
		window:  window,
	}
}

func (l *oauthRateLimiter) allow(key string) bool {
	if l == nil {
		return true
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = "anonymous"
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, entry := range l.entries {
		if now.Sub(entry.windowStart) >= l.window {
			delete(l.entries, existingKey)
		}
	}
	entry, ok := l.entries[key]
	if !ok || now.Sub(entry.windowStart) >= l.window {
		l.entries[key] = rateLimitEntry{windowStart: now, count: 1}
		return true
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func oauthClientIP(r *http.Request) string {
	if r == nil {
		return "anonymous"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) rateLimitOAuth(w http.ResponseWriter, r *http.Request, endpoint string, limiter RateLimitStore, limit int, window time.Duration) bool {
	if limiter == nil {
		return true
	}
	allowed, err := limiter.Allow(r.Context(), endpoint, oauthClientIP(r), limit, window, s.nowFn())
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "rate limit unavailable")
		return false
	}
	if !allowed {
		writeOAuthError(w, http.StatusTooManyRequests, "slow_down", "rate limit exceeded")
		return false
	}
	return true
}

func (s *Server) tokenRateLimiter(limit int, window time.Duration) RateLimitStore {
	if s == nil {
		return nil
	}
	if s.tokenLimiter != nil {
		return s.tokenLimiter
	}
	return newMemoryRateLimiter(limit, window)
}

func (s *Server) registerRateLimiter(limit int, window time.Duration) RateLimitStore {
	if s == nil {
		return nil
	}
	if s.registerLimiter != nil {
		return s.registerLimiter
	}
	return newMemoryRateLimiter(limit, window)
}

func rateLimitConfigFrom(cfg RateLimitConfig) (tokenLimit, registerLimit int, window time.Duration) {
	tokenLimit = cfg.TokenRequests
	registerLimit = cfg.RegisterRequests
	window = cfg.Window
	if tokenLimit <= 0 {
		tokenLimit = defaultOAuthTokenRateLimit
	}
	if registerLimit <= 0 {
		registerLimit = defaultOAuthRegisterRateLimit
	}
	if window <= 0 {
		window = defaultOAuthRateLimitWindow
	}
	return tokenLimit, registerLimit, window
}

// RateLimitConfig configures OAuth endpoint rate limits.
type RateLimitConfig struct {
	TokenRequests    int
	RegisterRequests int
	Window           time.Duration
}

func applyRateLimitConfig(s *Server, cfg RateLimitConfig) {
	if s == nil {
		return
	}
	tokenLimit, registerLimit, window := rateLimitConfigFrom(cfg)
	if s.tokenLimiter == nil {
		s.tokenLimiter = newMemoryRateLimiter(tokenLimit, window)
	}
	if s.registerLimiter == nil {
		s.registerLimiter = newMemoryRateLimiter(registerLimit, window)
	}
}
