# haistack-client (`pkg/client`)

Generic-first Go SDK for FHIR REST and HAIStack sync endpoints.

## What it does

**haistack-client** is the **outbound HTTP SDK** for Health AI Stack. It mirrors the FHIR REST surface exposed by `pkg/http` and adds focused sub-clients for sync, SMART, bulk export, and subscriptions.

Think of it as the client-side inverse of the server adapter:

| Concern | Owner |
|---------|-------|
| HTTP request construction and retries | `pkg/client` |
| Auth header injection (`TokenProvider`) | `pkg/client` |
| OperationOutcome error parsing | `pkg/client` |
| CRUD, search, bundles, metadata | `pkg/client` |
| HAIStack sync HTTP wire contract | `pkg/client` |
| SMART discovery, PKCE, client assertion (outbound) | `pkg/client` |
| FHIR bulk export kickoff/poll/manifest | `pkg/client` |
| FHIR Subscription REST helpers | `pkg/client` |
| FHIR JSON envelopes and codecs | `pkg/types` |
| Sync protocol models (transport-agnostic) | `pkg/sync` |
| FHIR REST server adapter | `pkg/http` |
| SMART token validation (server-side) | `pkg/smart` |
| Resource lifecycle and persistence | `pkg/core` |
| Search planning and execution | `pkg/search` |

It does **not**:

- Open databases or manage tenants (`pkg/sqlite`, `pkg/postgres`)
- Run sync scheduling or orchestration (`pkg/sync` Engine)
- Validate inbound SMART tokens (`pkg/smart` — use that on the server)
- Implement an OAuth authorization server
- Generate typed models for every FHIR resource

## When to use it

- Building Go applications, CLIs, or device agents that call a HAIStack FHIR server
- Bridging `pkg/sync.Engine` to a remote hub over HTTP push/pull
- Exchanging SMART tokens (auth-code+PKCE or backend client assertion) and attaching them to FHIR requests
- Integration tests that exercise outbound FHIR REST against `httptest` or real servers
- Bulk export workflows against standard FHIR bulk data endpoints

## Design

- **Envelope-first:** CRUD and search return `types.ResourceEnvelope` with normalized FHIR JSON.
- **Typed helpers are optional:** `Patient()`, `Observation()`, `Encounter()`, and `ForResource()` are thin wrappers — the generic API is primary.
- **R4 default:** FHIR version defaults to `4.0.1` unless server metadata advertises otherwise.
- **Sync boundary:** `SyncClient` owns HTTP transport; `pkg/sync` models are reused on the wire.
- **Explicit sync metadata:** `nodeId` and `tenantId` are required on every push/pull request.

## Supported endpoints

### FHIR REST

Base path defaults to `/fhir` (configurable via `Config.BasePath`).

| Method | Path | Action | Success |
|--------|------|--------|---------|
| `GET` | `/fhir/metadata` | Server capability statement | 200 + CapabilityStatement |
| `POST` | `/fhir` | Transaction bundle | 200 + transaction-response Bundle |
| `GET` | `/fhir/{ResourceType}` | Type-level search | 200 + searchset Bundle |
| `POST` | `/fhir/{ResourceType}` | Create resource | 201 + resource |
| `GET` | `/fhir/{ResourceType}/{id}` | Read resource | 200 + resource |
| `PUT` | `/fhir/{ResourceType}/{id}` | Update resource | 200 + resource |
| `DELETE` | `/fhir/{ResourceType}/{id}` | Delete resource | 204 No Content |
| `GET` | `/fhir/$export` | System bulk export kickoff | 202 + `Content-Location` |
| `GET` | `/fhir/Group/{id}/$export` | Group bulk export kickoff | 202 + `Content-Location` |

### HAIStack sync (wire contract)

| Method | Path | Action | Success |
|--------|------|--------|---------|
| `POST` | `/sync/push` | Propose `LocalEvent` batch | 200 + `PushResult` per event |
| `GET` | `/sync/pull` | Fetch `CanonicalEvent` batch | 200 + events + cursor |

Pull query parameters: `nodeId`, `tenantId`, `after` (cursor), `limit` (optional).

### Deferred (v1)

- `PATCH`, batch bundles, custom FHIR operations beyond bulk export
- `POST /{type}/_search` (only `GET` type-level search)
- R5-specific typed surfaces
- HAIStack-private subscription delivery/admin APIs

## Usage

### Construct a client

```go
import (
    "context"
    "log"
    "time"

    "github.com/degoke/health-ai-stack/pkg/client"
)

c, err := client.New(client.Config{
    BaseURL:       "https://fhir.example.com",
    BasePath:      "/fhir", // default
    TokenProvider: client.StaticTokenProvider{Token: accessToken},
    RetryPolicy: &client.DefaultRetryPolicy{
        Attempts:     5,
        InitialDelay: 500 * time.Millisecond,
        MaxDelay:     30 * time.Second,
    },
    DefaultHeaders: map[string]string{
        "X-Request-Source": "my-app",
    },
})
if err != nil {
    log.Fatal(err)
}
```

Use a custom `TokenProvider` when tokens refresh or vary per request:

```go
type refreshingProvider struct {
    smart *client.SMARTClient
    // ...
}

func (p *refreshingProvider) AuthorizationHeader(ctx context.Context) (string, error) {
  // Return "Bearer <token>" or empty when unauthenticated
}
```

### CRUD

```go
import (
    "github.com/degoke/health-ai-stack/pkg/types"
)

codec := types.NewJSONCodec()
env, err := codec.ParseJSON("Patient", []byte(`{"resourceType":"Patient","name":[{"family":"Smith"}]}`))
if err != nil {
    log.Fatal(err)
}

created, err := c.Create(ctx, env)
read, err := c.Read(ctx, "Patient", created.ID)

env.ID = created.ID
updated, err := c.Update(ctx, env)
err = c.Delete(ctx, "Patient", created.ID)
```

### Search and pagination

```go
// Single page
result, err := c.Search(ctx, "Patient", map[string]string{"family": "Smith"})
if result.HasNext() {
    next, err := c.SearchPage(ctx, "Patient", result.NextURL)
}

// Auto-pagination across all pages
all, err := c.SearchAll(ctx, "Patient", map[string]string{"family": "Smith"})

// Fluent builder with repeated params, OR values, _count, _sort
patients, err := c.SearchBuilder("Patient").
    Param("family", "Smith").
    AddParam("identifier", "mrn-1").
    OrParam("gender", "male", "female").
    Count(100).
    Sort("family", "-birthdate").
    SearchAll(ctx)
```

`SearchResult` exposes `Entries`, `Total`, `NextURL`, `SelfURL`, and `RawBundle` for advanced use.

### Transaction bundles

```go
bundle, err := client.NewTransactionBundleBuilder().
    CreateEntry(patientEnvelope).
    UpdateEntry(observationEnvelope).
    DeleteEntry("Encounter", "enc-1").
    Submit(ctx, c)
```

### Capability and version checks

```go
meta, err := c.Metadata(ctx)
version, err := c.CheckFHIRVersion(ctx)

supported, err := c.CheckFeatureSupport(ctx, "Patient", "search-type")
```

### Sync push/pull

Bridge `pkg/sync.Engine` to a remote hub. Request metadata includes node and tenant IDs explicitly:

```go
import "github.com/degoke/health-ai-stack/pkg/sync"

pushResp, err := c.Sync().Push(ctx, client.PushRequest{
    NodeID:   "device-node-1",
    TenantID: "tenant-a",
    Events:   []sync.LocalEvent{event},
})
results := client.FromPushResponse(pushResp)

pullResp, err := c.Sync().Pull(ctx, client.PullRequest{
    NodeID:   "device-node-1",
    TenantID: "tenant-a",
    After:    cursor,
    Limit:    100,
})
events := client.FromPullResponse(pullResp)
```

Or use the helper:

```go
pushResp, err := c.Sync().Push(ctx, client.ToPushRequest(nodeID, tenantID, events))
```

### SMART auth-code + PKCE

Outbound OAuth for interactive apps. Server-side token validation remains in `pkg/smart`.

```go
cfg, err := c.SMART().Discover(ctx, "https://fhir.example.com")
pkce, err := client.NewPKCEChallenge()

authURL, err := c.SMART().BuildAuthURL(client.AuthCodeRequest{
    Config:      cfg,
    ClientID:    "my-app",
    RedirectURI: "https://app.example/callback",
    Scope:       "launch/patient patient/*.read openid",
    State:       "csrf-token",
    PKCE:        pkce,
    Launch:      launchToken, // optional EHR launch context
})
// Redirect the user to authURL, capture the authorization code, then:
tokens, err := c.SMART().ExchangeAuthCode(ctx, cfg.TokenEndpoint,
    "my-app", "https://app.example/callback", code, pkce)

// Attach token to FHIR requests
c, _ = client.New(client.Config{
    BaseURL:       "https://fhir.example.com",
    TokenProvider: client.TokenProviderFromResponse(tokens),
})
```

Refresh when supported:

```go
refreshed, err := c.SMART().RefreshToken(ctx, cfg.TokenEndpoint, "my-app", tokens.RefreshToken)
```

### SMART backend client assertion

For system/service accounts using signed JWT assertions:

```go
tokens, err := c.SMART().ExchangeClientAssertion(ctx, client.ClientAssertionRequest{
    TokenEndpoint: cfg.TokenEndpoint,
    ClientID:      "backend-app",
    Scope:         "system/*.read",
    PrivateKey:    rsaPrivateKey,
    KeyID:         "key-1",
    Audience:      cfg.TokenEndpoint,
})
```

Parse claims without verification (validate separately with `pkg/smart` on the server):

```go
claims, err := client.ParseTokenClaims(tokens.AccessToken)
```

### Bulk export

Standard FHIR bulk data export (async):

```go
job, err := c.BulkExport().Kickoff(ctx, client.ExportKickoffRequest{
    ResourceTypes: []string{"Patient", "Observation"},
    Since:         time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
    OutputFormat:  "application/fhir+ndjson",
})

// Poll until complete
completed, err := c.BulkExport().Wait(ctx, job.StatusURL, 5*time.Second)

// Or poll manually
status, err := c.BulkExport().PollStatus(ctx, job.StatusURL)

// Retrieve manifest when complete
manifest, err := c.BulkExport().GetManifest(ctx, job.StatusURL)
for _, file := range manifest.Output {
    // file.Type, file.URL — NDJSON download URLs
}

// Cancel in-flight export
err = c.BulkExport().Cancel(ctx, job.StatusURL)
```

### Subscriptions

Standard FHIR Subscription REST (not HAIStack-private delivery APIs):

```go
sub, err := c.Subscriptions().Create(ctx, subscriptionEnvelope)
active, err := c.Subscriptions().Read(ctx, "sub-1")
err = c.Subscriptions().Delete(ctx, "sub-1")

results, err := c.Subscriptions().Search(ctx, map[string]string{"status": "active"})

// When server exposes a status endpoint
status, err := c.Subscriptions().PollStatus(ctx, "/fhir/Subscription/sub-1/$status")
```

### Typed helpers (optional)

Convenience wrappers over the generic API — not required:

```go
patient, err := c.Patient().Read(ctx, "p1")
obs, err := c.Observation().Search(ctx, map[string]string{"code": "8867-4"})
enc, err := c.Encounter().SearchAll(ctx, nil)

// Or bind any resource type
custom, err := c.ForResource("MedicationRequest").Read(ctx, "rx-1")
```

## Configuration reference

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `BaseURL` | **yes** | — | Server origin, e.g. `https://fhir.example.com` |
| `BasePath` | no | `/fhir` | FHIR REST prefix |
| `HTTPClient` | no | 30s timeout client | Underlying `*http.Client` |
| `Codec` | no | `types.NewJSONCodec()` | FHIR JSON parse/serialize |
| `TokenProvider` | no | nil | Authorization header per request |
| `RetryPolicy` | no | `DefaultRetryPolicy` | Retry attempts, backoff, jitter |
| `DefaultHeaders` | no | nil | Headers added to every request |
| `UserAgent` | no | `haistack-client/1.0` | User-Agent header |
| `Timeout` | no | `30s` | Applied when `HTTPClient` is nil |
| `FHIRVersion` | no | `4.0.1` | Default when metadata unavailable |

## Error handling

Failures return `*client.Error`. Use `client.AsError(err)` to unwrap:

```go
env, err := c.Read(ctx, "Patient", "missing")
if err != nil {
    if ce, ok := client.AsError(err); ok {
        log.Printf("status=%d retryable=%v", ce.StatusCode, ce.Retryable)
        if ce.Outcome != nil {
            for _, issue := range ce.Outcome.Issue {
                log.Printf("issue: %s — %s", issue.Code, issue.Diagnostics)
            }
        }
    }
}
```

| Field | Description |
|-------|-------------|
| `StatusCode` | HTTP response status |
| `Body` | Raw response bytes |
| `Outcome` | Parsed `types.OperationOutcome` when body is FHIR JSON |
| `Retryable` | Whether the request pipeline classified this as retryable |

Typical status mapping (aligned with `pkg/http` server behavior):

| HTTP status | Retryable (default) | Notes |
|-------------|---------------------|-------|
| 400 | no | Invalid request / query |
| 401 | no | Missing or invalid credentials |
| 403 | no | Authorization denied |
| 404 | no | Resource not found |
| 409 | no | Version conflict |
| 429 | yes | Rate limited |
| 5xx | yes | Server error |

## Retry behavior

`DefaultRetryPolicy` retries on transport errors, HTTP 429, and HTTP 5xx. Configure overrides:

```go
policy := &client.DefaultRetryPolicy{
    Attempts:     5,
    InitialDelay: 500 * time.Millisecond,
    MaxDelay:     30 * time.Second,
    JitterFraction: 0.2,
    RetryableStatus: map[int]bool{
        408: true, // retry request timeout
    },
}
```

Implement `RetryPolicy` for fully custom behavior.

## Package layout

| File | Role |
|------|------|
| `client.go` | `Client`, `New`, sub-client accessors, typed helpers |
| `config.go` | `Config` and defaults |
| `request.go` | Shared request pipeline, retries, content types |
| `errors.go` | `Error`, OperationOutcome parsing |
| `retry.go` | `RetryPolicy`, `DefaultRetryPolicy` |
| `auth.go` | `TokenProvider` implementations |
| `resources.go` | CRUD, `ResourceClient` typed wrapper |
| `search.go` | `Search`, `SearchBuilder`, pagination, `SearchResult` |
| `bundle.go` | `BundleBuilder` for transaction/batch bundles |
| `capability.go` | `Metadata`, `CheckFHIRVersion`, `CheckFeatureSupport` |
| `sync.go` | `SyncClient`, push/pull DTOs, bridge helpers |
| `smart.go` | SMART discovery, PKCE, token exchange, client assertion |
| `bulk.go` | Bulk export kickoff, poll, wait, cancel, manifest |
| `subscription.go` | Subscription CRUD/search/status polling |

## Testing

```bash
go test ./pkg/client/... -count=1
```

- **Unit tests** — CRUD request construction, error mapping, search builder, sync serialization, SMART PKCE/assertion, bulk export state machine, subscription CRUD
- **Integration tests** (`integration_test.go`) — end-to-end FHIR CRUD/search/pagination and sync over `httptest.Server`

## Related packages

| Package | Relationship |
|---------|--------------|
| `pkg/http` | Server-side FHIR REST adapter (inverse of this SDK) |
| `pkg/types` | `ResourceEnvelope`, `ResourceCodec`, `OperationOutcome` |
| `pkg/sync` | Transport-agnostic sync models; `Engine` orchestration |
| `pkg/smart` | Server-side token/assertion validation and scope parsing |
| `pkg/core` | Server-side resource lifecycle (not called by client) |
| `pkg/search` | Server-side search execution (not called by client) |

See [doc.go](./doc.go) for API detail, defaults, and non-goals.
