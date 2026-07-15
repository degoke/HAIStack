package client

import (
	"net/http"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

const (
	defaultBasePath    = "/fhir"
	defaultUserAgent   = "haistack-client/1.0"
	defaultFHIRVersion = "4.0.1"
	defaultTimeout     = 30 * time.Second
	defaultMaxAttempts = 3
)

// Config configures a Client.
type Config struct {
	// BaseURL is the server origin, e.g. https://fhir.example.com.
	BaseURL string
	// BasePath is the FHIR REST prefix. Defaults to /fhir.
	BasePath string
	// HTTPClient is the underlying transport. A default client with Timeout is used when nil.
	HTTPClient *http.Client
	// Codec parses and serializes FHIR JSON. Defaults to types.NewJSONCodec().
	Codec types.ResourceCodec
	// TokenProvider supplies authorization headers per request.
	TokenProvider TokenProvider
	// RetryPolicy controls retryability, attempts, backoff, and jitter.
	RetryPolicy RetryPolicy
	// DefaultHeaders are added to every request (auth headers from TokenProvider take precedence).
	DefaultHeaders map[string]string
	// UserAgent overrides the default haistack-client user agent.
	UserAgent string
	// Timeout applies when HTTPClient is nil.
	Timeout time.Duration
	// FHIRVersion is the default interoperability target (R4). Used when server metadata is unavailable.
	FHIRVersion string
}
