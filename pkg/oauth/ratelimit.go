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
	defaultOAuthLoginRateLimit    = 10
	defaultOAuthLoginIPRateLimit  = 30
	defaultOAuthRateLimitWindow   = time.Minute
)

// RateLimitStore persists or tracks OAuth endpoint request counters.
type RateLimitStore interface {
	Allow(ctx context.Context, endpoint, bucketKey string, limit int, window time.Duration, now time.Time) (bool, error)
}

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
	return s.rateLimitOAuthBucket(w, r, endpoint, oauthClientIP(r), limiter, limit, window)
}

func (s *Server) rateLimitOAuthBucket(w http.ResponseWriter, r *http.Request, endpoint, bucketKey string, limiter RateLimitStore, limit int, window time.Duration) bool {
	if limiter == nil {
		return true
	}
	now := time.Now()
	if s != nil && s.cfg.Now != nil {
		now = s.cfg.Now()
	}
	allowed, err := limiter.Allow(r.Context(), endpoint, bucketKey, limit, window, now)
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

func (s *Server) loginRateLimiter(limit int, window time.Duration) RateLimitStore {
	if s == nil {
		return nil
	}
	if s.loginLimiter != nil {
		return s.loginLimiter
	}
	return newMemoryRateLimiter(limit, window)
}

func (s *Server) loginIPRateLimiter(limit int, window time.Duration) RateLimitStore {
	if s == nil {
		return nil
	}
	if s.loginIPLimiter != nil {
		return s.loginIPLimiter
	}
	return newMemoryRateLimiter(limit, window)
}

func (s *Server) rateLimitLogin(w http.ResponseWriter, r *http.Request, username string) bool {
	_, _, loginLimit, loginIPLimit, window := rateLimitConfigFrom(s.cfg.RateLimit)
	ip := oauthClientIP(r)
	if !s.rateLimitOAuthBucket(w, r, "login-ip", ip, s.loginIPRateLimiter(loginIPLimit, window), loginIPLimit, window) {
		return false
	}
	userKey := ip + "\n" + strings.ToLower(strings.TrimSpace(username))
	return s.rateLimitOAuthBucket(w, r, "login", userKey, s.loginRateLimiter(loginLimit, window), loginLimit, window)
}

func rateLimitConfigFrom(cfg RateLimitConfig) (tokenLimit, registerLimit, loginLimit, loginIPLimit int, window time.Duration) {
	tokenLimit = cfg.TokenRequests
	registerLimit = cfg.RegisterRequests
	loginLimit = cfg.LoginRequests
	loginIPLimit = cfg.LoginIPRequests
	window = cfg.Window
	if tokenLimit <= 0 {
		tokenLimit = defaultOAuthTokenRateLimit
	}
	if registerLimit <= 0 {
		registerLimit = defaultOAuthRegisterRateLimit
	}
	if loginLimit <= 0 {
		loginLimit = defaultOAuthLoginRateLimit
	}
	if loginIPLimit <= 0 {
		loginIPLimit = defaultOAuthLoginIPRateLimit
	}
	if window <= 0 {
		window = defaultOAuthRateLimitWindow
	}
	return tokenLimit, registerLimit, loginLimit, loginIPLimit, window
}

// RateLimitConfig configures OAuth endpoint rate limits.
type RateLimitConfig struct {
	TokenRequests    int
	RegisterRequests int
	LoginRequests    int
	LoginIPRequests  int
	Window           time.Duration
}

func applyRateLimitConfig(s *Server, cfg RateLimitConfig, tokenStore, registerStore RateLimitStore) {
	if s == nil {
		return
	}
	tokenLimit, registerLimit, loginLimit, loginIPLimit, window := rateLimitConfigFrom(cfg)
	if tokenStore != nil {
		s.tokenLimiter = tokenStore
		if s.loginLimiter == nil {
			s.loginLimiter = tokenStore
		}
		if s.loginIPLimiter == nil {
			s.loginIPLimiter = tokenStore
		}
	} else if s.tokenLimiter == nil {
		s.tokenLimiter = newMemoryRateLimiter(tokenLimit, window)
	}
	if registerStore != nil {
		s.registerLimiter = registerStore
	} else if s.registerLimiter == nil {
		s.registerLimiter = newMemoryRateLimiter(registerLimit, window)
	}
	if s.loginLimiter == nil {
		s.loginLimiter = newMemoryRateLimiter(loginLimit, window)
	}
	if s.loginIPLimiter == nil {
		s.loginIPLimiter = newMemoryRateLimiter(loginIPLimit, window)
	}
}
