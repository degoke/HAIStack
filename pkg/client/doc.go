// Package client implements haistack-client, the generic-first Go SDK for FHIR REST
// and HAIStack sync endpoints.
//
// haistack-client is the outbound HTTP counterpart to pkg/http. It owns request
// construction, auth injection, retry/backoff, response decoding, OperationOutcome
// parsing, and focused sub-clients for SMART, bulk export, subscriptions, and sync.
// Resource lifecycle, persistence, indexing, and server behavior remain in lower
// layers.
//
// # Design principles
//
// Envelope-first API:
//
//   - CRUD and search return types.ResourceEnvelope with normalized FHIR JSON.
//   - Typed helpers for Patient, Observation, and Encounter are thin wrappers
//     over the generic API — not a separate transport stack.
//   - Callers work with canonical JSON bytes; generated FHIR structs are not required.
//
// Transport-only sync boundary:
//
//   - SyncClient defines HTTP wire shapes for POST /sync/push and GET /sync/pull.
//   - pkg/sync remains transport-agnostic; SDK DTOs mirror LocalEvent, PushResult,
//     and CanonicalEvent so pkg/sync.Engine callers can bridge without custom mapping.
//   - Node ID and tenant ID are explicit on every sync request; they are not inferred
//     from global client state alone.
//
// FHIR-first responses:
//
//   - application/fhir+json for FHIR payloads.
//   - Non-2xx responses decode into types.OperationOutcome when the body is FHIR JSON.
//   - 429 and 5xx are retryable by default; 4xx are not unless RetryPolicy overrides.
//   - SearchResult preserves raw bundle JSON for advanced callers.
//
// Shared request pipeline:
//
//   - All outbound calls flow through a single do() path: build request, inject
//     TokenProvider headers, apply DefaultHeaders, execute with retry/backoff,
//     decode success or map failure to client.Error.
//
// # Package boundaries
//
//   - pkg/client — outbound HTTP transport, SDK surface, sync wire contract
//   - pkg/http — inbound FHIR REST adapter (server-side inverse of this SDK)
//   - pkg/types — ResourceEnvelope, ResourceCodec, OperationOutcome
//   - pkg/sync — transport-agnostic sync models and Engine orchestration
//   - pkg/smart — server-side token/assertion validation and auth adaptation
//
// SMART token exchange lives in pkg/client (outbound OAuth). Token validation for
// inbound requests stays in pkg/smart; wire a TokenProvider from exchanged tokens.
//
// # Public API
//
// Core client:
//
//   - New(Config) (*Client, error) — construct the SDK client
//   - Client.Create / Read / Update / Delete — instance-level CRUD
//   - Client.Transaction — submit a transaction Bundle
//   - Client.Search / SearchPage / SearchAll — type-level search with pagination
//   - Client.SearchBuilder — fluent query composition over url.Values
//   - Client.Metadata / CheckFHIRVersion / CheckFeatureSupport — capability helpers
//   - NewTransactionBundleBuilder / NewBatchBundleBuilder — assemble request bundles
//
// Sub-clients (accessed via Client methods):
//
//   - Sync() *SyncClient — HAIStack sync push/pull over HTTP
//   - SMART() *SMARTClient — discovery, auth-code+PKCE, backend client assertion
//   - BulkExport() *BulkExportClient — kickoff, poll, cancel, manifest retrieval
//   - Subscriptions() *SubscriptionClient — standard FHIR Subscription REST helpers
//
// Typed convenience (optional wrappers):
//
//   - ForResource(type) *ResourceClient
//   - Patient() / Observation() / Encounter() *ResourceClient
//
// Configuration and infrastructure:
//
//   - Config — BaseURL, BasePath, HTTPClient, Codec, TokenProvider, RetryPolicy
//   - TokenProvider — AuthorizationHeader per request
//   - RetryPolicy / DefaultRetryPolicy — retryability, attempts, backoff, jitter
//   - Error — HTTP status, raw body, OperationOutcome, retry hints
//
// # Supported FHIR endpoints (v1)
//
// Base path defaults to /fhir (configurable via Config.BasePath).
//
//   - GET    /fhir/metadata                  — CapabilityStatement
//   - POST   /fhir                           — transaction Bundle
//   - GET    /fhir/{ResourceType}            — type-level search (searchset Bundle)
//   - POST   /fhir/{ResourceType}            — create
//   - GET    /fhir/{ResourceType}/{id}       — read
//   - PUT    /fhir/{ResourceType}/{id}       — update
//   - DELETE /fhir/{ResourceType}/{id}       — delete (204 No Content)
//   - GET    /fhir/$export                   — bulk export kickoff (async)
//   - GET    /fhir/Group/{id}/$export        — group bulk export kickoff
//
// Sync endpoints (HAIStack wire contract):
//
//   - POST   /sync/push                        — propose LocalEvent batch
//   - GET    /sync/pull?nodeId&tenantId&after — fetch CanonicalEvent batch
//
// # Configuration defaults
//
//   - BasePath: /fhir
//   - Codec: types.NewJSONCodec()
//   - RetryPolicy: DefaultRetryPolicy (3 attempts, 250ms initial backoff)
//   - HTTPClient timeout: 30s when HTTPClient is nil
//   - FHIRVersion: 4.0.1 (R4) when server metadata unavailable
//   - UserAgent: haistack-client/1.0
//
// # Error handling
//
// Failures return *client.Error (use AsError to unwrap):
//
//   - StatusCode — HTTP response status
//   - Body — raw response bytes
//   - Outcome — parsed types.OperationOutcome when body is FHIR JSON
//   - Retryable — hint from RetryPolicy / status code classification
//
// # Retry behavior
//
// DefaultRetryPolicy retries on:
//
//   - Transport errors (network failures, timeouts)
//   - HTTP 429 Too Many Requests
//   - HTTP 5xx server errors
//
// 4xx responses are not retried unless RetryableStatus overrides are configured.
//
// # Typical usage
//
//	c, err := client.New(client.Config{
//	    BaseURL:       "https://fhir.example.com",
//	    TokenProvider: client.StaticTokenProvider{Token: accessToken},
//	})
//
//	// CRUD
//	created, err := c.Create(ctx, envelope)
//	read, err := c.Read(ctx, "Patient", "p1")
//
//	// Search with auto-pagination
//	all, err := c.SearchBuilder("Patient").Param("family", "Smith").SearchAll(ctx)
//
//	// Sync bridge for pkg/sync.Engine
//	pushResp, err := c.Sync().Push(ctx, client.ToPushRequest(nodeID, tenantID, events))
//	pullResp, err := c.Sync().Pull(ctx, client.PullRequest{NodeID: nodeID, TenantID: tenantID, After: cursor})
//
//	// SMART auth-code + PKCE
//	cfg, _ := c.SMART().Discover(ctx, issuer)
//	pkce, _ := client.NewPKCEChallenge()
//	authURL, _ := c.SMART().BuildAuthURL(client.AuthCodeRequest{...})
//	tokens, _ := c.SMART().ExchangeAuthCode(ctx, cfg.TokenEndpoint, clientID, redirectURI, code, pkce)
//
// # Out of scope (v1)
//
//   - R5-specific typed surfaces
//   - HAIStack-private subscription delivery/admin APIs
//   - Automatic sync scheduling/runtime orchestration (use pkg/sync.Engine)
//   - Generated typed models for every FHIR resource
//   - OAuth authorization server implementation
//   - PATCH, batch bundles, custom operations beyond bulk export
//
// See README.md in this directory for endpoint tables, configuration reference,
// usage examples, and test guidance.
package client
