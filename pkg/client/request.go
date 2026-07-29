package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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

		var ce *Error
		if !errorsAsRetryable(err, &ce) {
			return nil, err
		}
		if ce != nil && !ce.Retryable {
			return nil, err
		}
		if !c.retryPolicy.ShouldRetry(attempt, httpStatusFromError(ce), transportErrFromError(err)) {
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
	for k, v := range c.defaultHeaders {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	for k, v := range opts.headers {
		req.Header.Set(k, v)
	}
	if !opts.skipAuth && c.tokenProvider != nil {
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

	retryable := c.retryPolicy.ShouldRetry(0, httpResp, nil)
	if !retryable {
		retryable = defaultRetryable(httpResp.StatusCode)
	}
	return nil, parseError(httpResp.StatusCode, body, retryable)
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
