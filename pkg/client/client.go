package client

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Client is the generic-first FHIR REST SDK.
type Client struct {
	baseURL        string
	basePath       string
	httpClient     *http.Client
	codec          types.ResourceCodec
	tokenProvider  TokenProvider
	retryPolicy    RetryPolicy
	defaultHeaders map[string]string
	userAgent      string
	fhirVersion    string

	syncClient         *SyncClient
	smartClient        *SMARTClient
	bulkExportClient   *BulkExportClient
	subscriptionClient *SubscriptionClient
}

// New constructs a Client from Config.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("client: BaseURL is required")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	parsedBase, err := url.Parse(baseURL)
	if err != nil || parsedBase.Scheme == "" || parsedBase.Host == "" || parsedBase.User != nil || parsedBase.RawQuery != "" || parsedBase.Fragment != "" || (parsedBase.Path != "" && parsedBase.Path != "/") {
		return nil, fmt.Errorf("client: BaseURL must be an HTTP server origin without a path, query, or fragment")
	}
	if !strings.EqualFold(parsedBase.Scheme, "http") && !strings.EqualFold(parsedBase.Scheme, "https") {
		return nil, fmt.Errorf("client: BaseURL must use http or https")
	}
	basePath := cfg.BasePath
	if basePath == "" {
		basePath = defaultBasePath
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	basePath = strings.TrimRight(basePath, "/")

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = defaultTimeout
		}
		httpClient = &http.Client{Timeout: timeout}
	}

	codec := cfg.Codec
	if codec == nil {
		codec = types.NewJSONCodec()
	}

	retryPolicy := cfg.RetryPolicy
	if retryPolicy == nil {
		retryPolicy = NewDefaultRetryPolicy()
	}

	fhirVersion := cfg.FHIRVersion
	if fhirVersion == "" {
		fhirVersion = defaultFHIRVersion
	}

	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	c := &Client{
		baseURL:        baseURL,
		basePath:       basePath,
		httpClient:     httpClient,
		codec:          codec,
		tokenProvider:  cfg.TokenProvider,
		retryPolicy:    retryPolicy,
		defaultHeaders: copyHeaders(cfg.DefaultHeaders),
		userAgent:      userAgent,
		fhirVersion:    fhirVersion,
	}
	c.syncClient = &SyncClient{client: c}
	c.smartClient = &SMARTClient{client: c}
	c.bulkExportClient = &BulkExportClient{client: c}
	c.subscriptionClient = &SubscriptionClient{client: c, resourceType: "Subscription"}
	return c, nil
}

// BaseURL returns the configured server origin.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// BasePath returns the configured FHIR REST prefix.
func (c *Client) BasePath() string {
	if c == nil {
		return ""
	}
	return c.basePath
}

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	return c.httpClient
}

// Codec returns the resource codec.
func (c *Client) Codec() types.ResourceCodec {
	if c == nil {
		return nil
	}
	return c.codec
}

// FHIRVersion returns the default FHIR version target.
func (c *Client) FHIRVersion() string {
	if c == nil {
		return ""
	}
	return c.fhirVersion
}

// Sync returns the HAIStack sync sub-client.
func (c *Client) Sync() *SyncClient {
	if c == nil {
		return nil
	}
	return c.syncClient
}

// SMART returns the SMART OAuth sub-client.
func (c *Client) SMART() *SMARTClient {
	if c == nil {
		return nil
	}
	return c.smartClient
}

// BulkExport returns the FHIR bulk export sub-client.
func (c *Client) BulkExport() *BulkExportClient {
	if c == nil {
		return nil
	}
	return c.bulkExportClient
}

// Subscriptions returns the FHIR Subscription sub-client.
func (c *Client) Subscriptions() *SubscriptionClient {
	if c == nil {
		return nil
	}
	return c.subscriptionClient
}

// ForResource returns a typed convenience client for the given resource type.
func (c *Client) ForResource(resourceType string) *ResourceClient {
	return &ResourceClient{client: c, resourceType: resourceType}
}

// Patient returns a convenience client for Patient resources.
func (c *Client) Patient() *ResourceClient {
	return c.ForResource("Patient")
}

// Observation returns a convenience client for Observation resources.
func (c *Client) Observation() *ResourceClient {
	return c.ForResource("Observation")
}

// Encounter returns a convenience client for Encounter resources.
func (c *Client) Encounter() *ResourceClient {
	return c.ForResource("Encounter")
}

func (c *Client) fhirURL(parts ...string) string {
	path := c.basePath
	for _, part := range parts {
		if part == "" {
			continue
		}
		path += "/" + strings.Trim(part, "/")
	}
	return c.baseURL + path
}

func copyHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
