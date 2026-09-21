package bulkimport

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// HTTPLoaderTimeout is the default client timeout for remote NDJSON fetches.
	HTTPLoaderTimeout = 60 * time.Second
	// HTTPLoaderMaxBytes is the default maximum size of a fetched NDJSON payload.
	HTTPLoaderMaxBytes int64 = 64 << 20
)

// HTTPLoader fetches import input.url values over HTTP(S).
//
// Only http and https URLs are accepted. file://, data:, and other schemes are
// rejected before the request is issued, and redirects to those schemes are
// refused. The default client uses a 60s timeout and reads at most 64MiB.
type HTTPLoader struct {
	Client   *http.Client
	MaxBytes int64
}

// NewHTTPLoader returns an HTTPLoader with a 60s timeout.
func NewHTTPLoader() *HTTPLoader {
	return &HTTPLoader{
		Client: &http.Client{
			Timeout:       HTTPLoaderTimeout,
			CheckRedirect: httpLoaderRedirect,
		},
		MaxBytes: HTTPLoaderMaxBytes,
	}
}

var _ URLLoader = (*HTTPLoader)(nil)

// Load GETs an http or https URL and returns the response body.
func (l *HTTPLoader) Load(ctx context.Context, rawURL string) ([]byte, error) {
	if l == nil {
		return nil, fmt.Errorf("import: HTTP loader is nil")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("import: invalid url: %w", err)
	}
	if err := allowedImportURLScheme(parsed.Scheme); err != nil {
		return nil, err
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("import: url host is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("import: build request: %w", err)
	}
	resp, err := l.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("import: get %s: %w", parsed.Redacted(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("import: get %s: unexpected status %d", parsed.Redacted(), resp.StatusCode)
	}
	maxBytes := l.MaxBytes
	if maxBytes <= 0 {
		maxBytes = HTTPLoaderMaxBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("import: read %s: %w", parsed.Redacted(), err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("import: response from %s exceeds %d bytes", parsed.Redacted(), maxBytes)
	}
	return data, nil
}

func (l *HTTPLoader) client() *http.Client {
	if l != nil && l.Client != nil {
		return l.Client
	}
	return &http.Client{
		Timeout:       HTTPLoaderTimeout,
		CheckRedirect: httpLoaderRedirect,
	}
}

func httpLoaderRedirect(req *http.Request, via []*http.Request) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("import: redirect missing url")
	}
	if err := allowedImportURLScheme(req.URL.Scheme); err != nil {
		return err
	}
	if len(via) >= 10 {
		return fmt.Errorf("import: too many redirects")
	}
	return nil
}

func allowedImportURLScheme(scheme string) error {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "https":
		return nil
	case "":
		return fmt.Errorf("import: url scheme is required; only http and https are allowed")
	default:
		return fmt.Errorf("import: url scheme %q is not allowed; only http and https are allowed", scheme)
	}
}
