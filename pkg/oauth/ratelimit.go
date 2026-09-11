package oauth

import (
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

func (s *Server) rateLimitOAuth(w http.ResponseWriter, r *http.Request, limiter *oauthRateLimiter) bool {
	if limiter == nil || !limiter.allow(oauthClientIP(r)) {
		if limiter != nil {
			writeOAuthError(w, http.StatusTooManyRequests, "slow_down", "rate limit exceeded")
			return false
		}
	}
	return true
}

func (s *Server) tokenRateLimiter() *oauthRateLimiter {
	if s == nil {
		return nil
	}
	if s.tokenLimiter == nil {
		s.tokenLimiter = newOAuthRateLimiter(defaultOAuthTokenRateLimit, defaultOAuthRateLimitWindow)
	}
	return s.tokenLimiter
}

func (s *Server) registerRateLimiter() *oauthRateLimiter {
	if s == nil {
		return nil
	}
	if s.registerLimiter == nil {
		s.registerLimiter = newOAuthRateLimiter(defaultOAuthRegisterRateLimit, defaultOAuthRateLimitWindow)
	}
	return s.registerLimiter
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
	s.tokenLimiter = newOAuthRateLimiter(tokenLimit, window)
	s.registerLimiter = newOAuthRateLimiter(registerLimit, window)
}
