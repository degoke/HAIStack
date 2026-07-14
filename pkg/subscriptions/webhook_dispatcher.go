package subscriptions

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// WebhookResult captures one HTTP delivery attempt.
type WebhookResult struct {
	StatusCode int
	Body       string
}

// WebhookDispatcher performs HTTP webhook delivery.
type WebhookDispatcher struct {
	Client *http.Client
}

// Dispatch sends the configured webhook request.
func (d *WebhookDispatcher) Dispatch(ctx context.Context, cfg WebhookConfig, event DeliverPayload, resource *types.ResourceEnvelope) (WebhookResult, error) {
	if cfg.URL == "" {
		return WebhookResult{}, fmt.Errorf("%w: webhook url is required", ErrInvalidChannel)
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodPost
	}
	var body io.Reader
	if cfg.PayloadMode != PayloadModeEventOnly {
		if resource != nil && len(resource.JSON) > 0 {
			body = bytes.NewReader(resource.JSON)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, body)
	if err != nil {
		return WebhookResult{}, err
	}
	if body != nil {
		if ct := contentTypeForPayload(cfg); ct != "" {
			req.Header.Set("Content-Type", ct)
		}
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	client := d.client()
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return WebhookResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	result := WebhookResult{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return result, nil
}

func (d *WebhookDispatcher) client() *http.Client {
	if d != nil && d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

func contentTypeForPayload(cfg WebhookConfig) string {
	for k, v := range cfg.Headers {
		if strings.EqualFold(k, "Content-Type") {
			return v
		}
	}
	return "application/fhir+json"
}
