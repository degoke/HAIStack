package http

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimitConfig enables a process-local fixed-window request limiter.
// Requests <= 0 leaves rate limiting disabled. Key should identify the caller;
// when omitted, the remote IP address is used.
type RateLimitConfig struct {
	Requests   int
	Window     time.Duration
	Key        func(*http.Request) string
	MaxEntries int
}

type rateLimitEntry struct {
	windowStart time.Time
	count       int
	lastSeen    time.Time
}

type rateLimiter struct {
	config  RateLimitConfig
	mu      sync.Mutex
	entries map[string]rateLimitEntry
}

type rateLimitError struct {
	retryAfter time.Duration
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded; retry after %s", e.retryAfter.Round(time.Second))
}

// NewRateLimitMiddleware returns middleware enforcing cfg at the HTTP edge.
// It deliberately runs outside authentication when installed by NewHandler,
// so unauthenticated traffic cannot consume expensive auth work indefinitely.
func NewRateLimitMiddleware(cfg RateLimitConfig) func(http.Handler) http.Handler {
	if cfg.Requests <= 0 || cfg.Window <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 10000
	}
	limiter := &rateLimiter{config: cfg, entries: make(map[string]rateLimitEntry)}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			format, err := negotiateResponseFormat(r)
			if err != nil {
				writeError(w, err)
				return
			}
			w = withResponseFormat(w, format)
			allowed, remaining, retryAfter := limiter.allow(r)
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", cfg.Requests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
			if !allowed {
				seconds := int(retryAfter.Seconds())
				if retryAfter > time.Duration(seconds)*time.Second {
					seconds++
				}
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
				writeError(w, &rateLimitError{retryAfter: retryAfter})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (l *rateLimiter) allow(r *http.Request) (bool, int, time.Duration) {
	now := time.Now()
	key := "anonymous"
	if l.config.Key != nil {
		key = strings.TrimSpace(l.config.Key(r))
	} else {
		key = defaultRateLimitKey(r)
	}
	if key == "" {
		key = "anonymous"
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	for existingKey, entry := range l.entries {
		if now.Sub(entry.windowStart) >= l.config.Window {
			delete(l.entries, existingKey)
		}
	}
	entry, ok := l.entries[key]
	if !ok && len(l.entries) >= l.config.MaxEntries {
		var oldestKey string
		var oldest time.Time
		for existingKey, existing := range l.entries {
			if oldestKey == "" || existing.lastSeen.Before(oldest) {
				oldestKey = existingKey
				oldest = existing.lastSeen
			}
		}
		if oldestKey != "" {
			delete(l.entries, oldestKey)
		}
	}
	if !ok || now.Sub(entry.windowStart) >= l.config.Window {
		l.entries[key] = rateLimitEntry{windowStart: now, count: 1, lastSeen: now}
		return true, l.config.Requests - 1, 0
	}
	if entry.count >= l.config.Requests {
		retryAfter := l.config.Window - now.Sub(entry.windowStart)
		return false, 0, retryAfter
	}
	entry.count++
	entry.lastSeen = now
	l.entries[key] = entry
	return true, l.config.Requests - entry.count, 0
}

func defaultRateLimitKey(r *http.Request) string {
	if r == nil {
		return "anonymous"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
