package client

import (
	"math/rand"
	"net/http"
	"time"
)

// RetryPolicy decides retryability, max attempts, backoff, and jitter.
type RetryPolicy interface {
	// ShouldRetry reports whether the request should be retried for the given attempt (0-based).
	ShouldRetry(attempt int, resp *http.Response, err error) bool
	// MaxAttempts returns the maximum number of attempts including the first.
	MaxAttempts() int
	// Backoff returns the delay before the next attempt.
	Backoff(attempt int) time.Duration
}

// DefaultRetryPolicy retries 429 and 5xx responses and transport errors.
type DefaultRetryPolicy struct {
	Attempts       int
	InitialDelay   time.Duration
	MaxDelay       time.Duration
	JitterFraction float64
	// RetryableStatus overrides default retry classification for specific status codes.
	RetryableStatus map[int]bool
}

// NewDefaultRetryPolicy returns a policy with sensible defaults.
func NewDefaultRetryPolicy() *DefaultRetryPolicy {
	return &DefaultRetryPolicy{
		Attempts:       defaultMaxAttempts,
		InitialDelay:   250 * time.Millisecond,
		MaxDelay:       10 * time.Second,
		JitterFraction: 0.2,
	}
}

// MaxAttempts implements RetryPolicy.
func (p *DefaultRetryPolicy) MaxAttempts() int {
	if p == nil || p.Attempts <= 0 {
		return defaultMaxAttempts
	}
	return p.Attempts
}

// ShouldRetry implements RetryPolicy.
func (p *DefaultRetryPolicy) ShouldRetry(attempt int, resp *http.Response, err error) bool {
	if attempt+1 >= p.MaxAttempts() {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	if p != nil && p.RetryableStatus != nil {
		if retry, ok := p.RetryableStatus[resp.StatusCode]; ok {
			return retry
		}
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// Backoff implements RetryPolicy.
func (p *DefaultRetryPolicy) Backoff(attempt int) time.Duration {
	base := 250 * time.Millisecond
	max := 10 * time.Second
	jitter := 0.2
	if p != nil {
		if p.InitialDelay > 0 {
			base = p.InitialDelay
		}
		if p.MaxDelay > 0 {
			max = p.MaxDelay
		}
		if p.JitterFraction > 0 {
			jitter = p.JitterFraction
		}
	}
	if jitter > 1 {
		jitter = 1
	}
	delay := base * time.Duration(1<<attempt)
	if delay > max {
		delay = max
	}
	if jitter > 0 {
		span := float64(delay) * jitter
		delta := (rand.Float64()*2 - 1) * span
		delay = time.Duration(float64(delay) + delta)
		if delay < 0 {
			delay = 0
		}
	}
	return delay
}
