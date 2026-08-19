# haistack-http (`pkg/http`)

FHIR REST API adapter for the health-ai-stack monorepo.

## What it does

**haistack-http** is the **HTTP transport layer** for Health AI Stack. It exposes a FHIR REST surface over existing services without absorbing lifecycle, storage, indexing, sync, or AI behavior.

| Concern | Owner |
|---------|-------|
| HTTP routing and method validation | `pkg/http` |
| Request/response JSON translation | `pkg/http` |
| OperationOutcome error rendering | `pkg/http` |
| CapabilityStatement from registry snapshot | `pkg/http` |
| Pluggable auth middleware | `pkg/http` |
| CRUD, history, transaction bundles | `pkg/core` |
| Search planning and execution | `pkg/search` |
| Enabled resource types and SearchParameters | `pkg/registry` |
| Policy decisions | `pkg/auth` |

It does **not**:

- Open databases or manage tenants (`pkg/sqlite`, `pkg/postgres`)
- Index resources or compile search plans (`pkg/search` write path)
- Validate profiles or assign version IDs (`pkg/core`, `pkg/types`)
- Issue or validate SMART/OAuth tokens (`pkg/smart`) — callers wire token resolution into `PrincipalResolver`

## Import alias

This package is named `http` and shadows the standard library. Always import with an alias:

```go
import (
    nethttp "net/http"

    hahttp "github.com/degoke/health-ai-stack/pkg/http"
)
```

## Supported endpoints (MVP)

Base path defaults to `/fhir` (configurable via `Config.BasePath`).

| Method | Path | Action | Success |
|--------|------|--------|---------|
| `GET` | `/fhir/metadata` | Server capability statement | 200 + CapabilityStatement |
| `POST` | `/fhir` | Transaction or batch bundle | 200 + response Bundle |
| `GET` | `/fhir/{ResourceType}` | Type-level search | 200 + searchset Bundle |
| `POST` | `/fhir/{ResourceType}/_search` | POST search | 200 + searchset Bundle |
| `POST` | `/fhir/{ResourceType}` | Create (or conditional create) | 201 or 200 + resource |
| `PUT` | `/fhir/{ResourceType}?...` | Conditional update | 201/200 + resource |
| `DELETE` | `/fhir/{ResourceType}?...` | Conditional delete | 204 No Content |
| `GET` | `/fhir/{ResourceType}/{id}` | Read resource | 200 + resource, `ETag`, `Last-Modified` |
| `PUT` | `/fhir/{ResourceType}/{id}` | Update resource | 200 + resource |
| `PATCH` | `/fhir/{ResourceType}/{id}` | JSON Patch update | 200 + resource |
| `DELETE` | `/fhir/{ResourceType}/{id}` | Delete resource | 204 No Content |
| `GET` | `/fhir/{ResourceType}/{id}/_history` | Instance history | 200 + history Bundle |
| `GET` | `/fhir/$export` | System bulk export kickoff | 501 Not Implemented |
| `GET` | `/fhir/Group/{id}/$export` | Group bulk export kickoff | 501 Not Implemented |
| `GET`/`POST` | `/fhir/$operation` or resource operation path | Custom operation | 200 + returned resource |
| `POST` | `/sync/push` | Sync push (via `NewRootHandlerWithSyncMiddleware`) | 200 + results |
| `GET` | `/sync/pull` | Sync pull (via `NewRootHandlerWithSyncMiddleware`) | 200 + events |

`POST /fhir` accepts transaction and batch Bundles. JSON projections
(`_summary`, `_elements`) and JSON/XML content negotiation are supported.
Unsupported `Accept` or `_format` values return 406. JSON and XML request bodies
are accepted for resource and Bundle writes.

Set `Config.RateLimit` to enable a process-local fixed-window limiter. It emits
429 `OperationOutcome` responses with `Retry-After` and rate-limit headers;
multi-instance deployments should enforce an equivalent limit at a shared edge.

When exposing sync routes, apply authentication with
`NewRootHandlerWithSyncMiddleware` or `RootConfig.SyncMiddleware`. A
tenant-aware hub should implement `sync.ScopedHubServer`; the Postgres hub
also requires a registered node for the tenant.

SDC operations (`$populate`, `$assemble`, `$validate`, `$extract`, adaptive questionnaire routes) are supported on `Questionnaire` and `QuestionnaireResponse`.

### Deferred

- Bulk export implementation (routes return 501)
- Full CapabilityStatement conformance coverage
- SMART metadata and built-in token runtime

## When to use it

- Serving a FHIR REST API from a Go binary or sidecar
- Integration tests that exercise end-to-end JSON translation over real stores
- Wiring SMART, API-key, or service-account auth via `PrincipalResolver` without changing core/search APIs

## Usage

### Minimal handler (CRUD only)

```go
handler, err := hahttp.NewHandler(hahttp.Config{
    ResourceService: hahttp.CoreResourceService{Svc: coreSvc},
})
if err != nil {
    log.Fatal(err)
}
nethttp.ListenAndServe(":8080", handler)
```

### Full stack (CRUD + search + metadata + auth)

```go
snapshot, _ := manager.RebuildSnapshot(ctx)

handler, err := hahttp.NewHandler(hahttp.Config{
    BasePath:         "/fhir",
    ResourceService:  hahttp.CoreResourceService{Svc: coreSvc},
    SearchService:    hahttp.SearchServiceAdapter{Svc: searchSvc},
    CapabilitySource: hahttp.RegistryCapabilitySource{Snapshot: snapshot},
    ServerMetadata: hahttp.ServerMetadata{
        SoftwareName:    "my-fhir-server",
        SoftwareVersion: "1.0.0",
        ServerName:      "Production FHIR Server",
    },
    PrincipalResolver: func(ctx context.Context, r *nethttp.Request) (auth.Principal, auth.TenantContext, error) {
        // Resolve from Authorization header, session, SMART token, etc.
        return principal, tenant, nil
    },
    AuthChecker: hahttp.PolicyAuthChecker{Engine: authEngine},
})
```

When `PrincipalResolver` and `AuthChecker` are both set, built-in middleware runs before handlers. When neither is set, all requests pass through. Set `AuthMiddleware` instead for fully custom wrapping.

### Custom service implementations

Implement the narrow interfaces directly when you need mocks or alternate backends:

```go
type myResources struct{}

func (m myResources) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) { ... }
func (m myResources) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) { ... }
// ... Update, Delete, History, ProcessTransactionBundle, ProcessBatchBundle, Patch
```

## Configuration reference

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `BasePath` | no | `/fhir` | FHIR REST root path |
| `ResourceService` | **yes** | — | CRUD, history, transaction/batch bundles, JSON Patch |
| `SearchService` | no | nil | Enables `GET /{type}` search |
| `CapabilitySource` | no | nil | Enables `GET /metadata` |
| `ServerMetadata` | no | empty | Software/server fields in CapabilityStatement |
| `Codec` | no | `types.NewJSONCodec()` | FHIR JSON parse/serialize |
| `AuthMiddleware` | no | nil | Custom outer middleware |
| `PrincipalResolver` | no | nil | Identity extraction when auth enabled |
| `AuthChecker` | no | nil | Read/write/search authorization |
| `OperationService` | no | nil | Generic custom `$operation` execution |
| `RateLimit` | no | disabled | Process-local fixed-window request limiter |

## Error responses

Handler failures return negotiated FHIR JSON or XML `OperationOutcome` bodies.

| Source | HTTP status | Issue code (typical) |
|--------|-------------|----------------------|
| `core.ErrorKindInvalid` | 400 | `invalid` |
| `core.ErrorKindNotFound` | 404 | `not-found` |
| `core.ErrorKindConflict` | 409 | `conflict` |
| `core.ErrorKindNotSupported` | 400 | `not-supported` |
| `core.ErrorKindException` | 500 | `exception` |
| `search.ErrInvalidQuery` and related | 400 | `invalid` |
| `auth.ErrDenied` | 403 | `forbidden` |
| Missing credentials (auth enabled) | 401 | `security` |
| Unsupported HTTP method | 405 | `not-supported` |
| Rate limit exceeded | 429 | `throttled` |

Path/body validation at the HTTP layer (malformed ids, id mismatch on update, non-transaction POST to `/fhir`) is mapped to `invalid` or `not-supported` before reaching core.

## Response headers

| Header | When set |
|--------|----------|
| `Content-Type: application/fhir+json` / `application/fhir+xml` | Negotiated FHIR responses |
| `Location` | 201 Created |
| `ETag: W/"{versionId}"` | Read/update when version metadata present |
| `Last-Modified` | Read/update when `LastUpdated` present |

## Auth integration

`PolicyAuthChecker` delegates to `auth.PolicyEngine`:

- **Read** → `CanReadResource`
- **Write** (create/update/delete) → `CanWriteResource`
- **Search** → `CanReadResource` on the resource type

For SMART-backed servers, resolve tokens in `PrincipalResolver` and optionally use `pkg/smart.AuthAdapter` to build `auth.ReadRequest` / `auth.WriteRequest` inside a custom `AuthChecker`.

## Package layout

| File | Role |
|------|------|
| `config.go` | `NewHandler`, `Config`, auth types |
| `service.go` | `ResourceService`, `SearchService`, `CapabilitySource` |
| `adapter.go` | Adapters for core, search, registry, auth |
| `handler.go` | Route dispatch and endpoint handlers |
| `router.go` | Path parsing and ID validation |
| `errors.go` | Error → status + OperationOutcome |
| `writer.go` | Response writers and FHIR headers |
| `bundle.go` | History, searchset, CapabilityStatement JSON |
| `auth.go` | Built-in auth middleware |
| `request.go` | Body parsing |

## Testing

```bash
go test ./pkg/http/... -count=1
```

- **Unit tests** (`http_test.go`) — mock services for every endpoint, auth paths, and error mapping
- **Integration tests** (`integration_test.go`) — sqlite-backed core + search wired through adapters for end-to-end JSON translation

## Related packages

| Package | Relationship |
|---------|--------------|
| `pkg/core` | Resource lifecycle delegated via `ResourceService` |
| `pkg/search` | Type-level search via `SearchService` |
| `pkg/registry` | Capability snapshot for `/metadata` |
| `pkg/auth` | Optional authorization via `AuthChecker` |
| `pkg/smart` | Token/scope → principal (caller-wired, not built-in) |
| `pkg/types` | `ResourceEnvelope`, `ResourceCodec`, `OperationOutcome` |
