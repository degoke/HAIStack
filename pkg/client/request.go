package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	fhirJSONContentType = "application/fhir+json"
	fhirJSONAccept      = "application/fhir+json"
)

// rawResponse preserves HTTP metadata for advanced callers.
type rawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// requestOptions configures a single outbound request.
type requestOptions struct {
	method      string
	url         string
	body        []byte
	contentType string
	accept      string
	headers     map[string]string
	skipAuth    bool
	expectEmpty bool
}

func (c *Client) do(ctx context.Context, opts requestOptions) (*rawResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	var lastErr error
	maxAttempts := c.retryPolicy.MaxAttempts()
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	retryEligible := opts.method == http.MethodGet || opts.method == http.MethodHead || opts.method == http.MethodOptions || opts.method == http.MethodPut || opts.method == http.MethodDelete || opts.headers["Idempotency-Key"] != ""
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := c.retryPolicy.Backoff(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := c.doOnce(ctx, opts)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if !retryEligible {
			return nil, err
		}

		var ce *Error
		if !errorsAsRetryable(err, &ce) {
			return nil, err
		}
		shouldRetry := c.retryPolicy.ShouldRetry(attempt, httpStatusFromError(ce), transportErrFromError(err))
		if !shouldRetry {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) doOnce(ctx context.Context, opts requestOptions) (*rawResponse, error) {
	var bodyReader io.Reader
	if len(opts.body) > 0 {
		bodyReader = bytes.NewReader(opts.body)
	}
	req, err := http.NewRequestWithContext(ctx, opts.method, opts.url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", c.userAgent)
	if opts.accept == "" {
		req.Header.Set("Accept", fhirJSONAccept)
	} else {
		req.Header.Set("Accept", opts.accept)
	}
	if len(opts.body) > 0 {
		ct := opts.contentType
		if ct == "" {
			ct = fhirJSONContentType
		}
		req.Header.Set("Content-Type", ct)
	}
	crossOrigin := !sameOrigin(c.baseURL, opts.url)
	for k, v := range c.defaultHeaders {
		if crossOrigin && (strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Cookie")) {
			continue
		}
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}
	if !opts.skipAuth && !crossOrigin && c.tokenProvider != nil {
		auth, err := c.tokenProvider.AuthorizationHeader(ctx)
		if err != nil {
			return nil, fmt.Errorf("auth: %w", err)
		}
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = httpResp.Body.Close() }()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	raw := &rawResponse{
		StatusCode: httpResp.StatusCode,
		Header:     httpResp.Header.Clone(),
		Body:       body,
	}

	if isSuccessStatus(httpResp.StatusCode) {
		if opts.expectEmpty && len(body) == 0 {
			return raw, nil
		}
		return raw, nil
	}

	return nil, parseError(httpResp.StatusCode, body, defaultRetryable(httpResp.StatusCode))
}

func sameOrigin(base, target string) bool {
	baseURL, err := url.Parse(base)
	if err != nil {
		return false
	}
	targetURL, err := url.Parse(target)
	if err != nil {
		return false
	}
	if targetURL.Scheme == "" && targetURL.Host == "" {
		return true
	}
	return targetURL.User == nil && strings.EqualFold(baseURL.Scheme, targetURL.Scheme) && strings.EqualFold(baseURL.Host, targetURL.Host)
}

// resolveSameOriginURL resolves a server-provided relative URL and rejects
// absolute URLs that would send credentials or data to another origin.
func resolveSameOriginURL(base, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", fmt.Errorf("server URL is empty")
	}
	resolved := target
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		if strings.HasPrefix(target, "/") {
			resolved = base + target
		} else {
			resolved = base + "/" + target
		}
	}
	if !sameOrigin(base, resolved) {
		return "", fmt.Errorf("server URL is outside the configured server origin")
	}
	parsed, err := url.Parse(resolved)
	if err != nil || parsed.User != nil {
		return "", fmt.Errorf("server URL is invalid")
	}
	return resolved, nil
}

func errorsAsRetryable(err error, ce **Error) bool {
	if err == nil {
		return false
	}
	if e, ok := AsError(err); ok {
		*ce = e
		return true
	}
	return true
}

func httpStatusFromError(ce *Error) *http.Response {
	if ce == nil {
		return nil
	}
	return &http.Response{StatusCode: ce.StatusCode}
}

func transportErrFromError(err error) error {
	if _, ok := AsError(err); ok {
		return nil
	}
	return err
}
