# haistack-http (`pkg/http`)

FHIR REST API adapter for the haistack monorepo.

## What it does

**haistack-http** is the **HTTP transport layer** for HAIStack. It exposes a FHIR REST surface over existing services without absorbing lifecycle, storage, indexing, sync, or AI behavior.

| Concern | Owner |
|---------|-------|
| HTTP routing and method validation | `pkg/http` |
| Request/response JSON and XML translation | `pkg/http` |
| OperationOutcome error rendering | `pkg/http` |
| CapabilityStatement from registry snapshot | `pkg/http` |
| Pluggable auth middleware and SMART scope filters | `pkg/http` |
| CRUD, history, transaction bundles | `pkg/core` |
| Search planning and execution | `pkg/search` |
| Enabled resource types and SearchParameters | `pkg/registry` |
| Policy decisions | `pkg/auth` |

Entrypoints include `NewHandler(Config)`, optional `NewRootHandlerWithSyncMiddleware` for sync routes, and health probes `/health` and `/healthz`.

It does **not**:

- Open databases or manage tenants (`pkg/sqlite`, `pkg/postgres`)
- Index resources or compile search plans (`pkg/search` write path)
- Validate profiles or assign version IDs (`pkg/core`, `pkg/types`)
- Issue or validate SMART/OAuth tokens (`pkg/smart`, `pkg/oauth`) — callers wire token resolution into `PrincipalResolver`

## How it fits in the ecosystem

```
  Client (FHIR REST, Bulk Data, SDC ops)
           |
           v
  pkg/http (router, negotiation, errors, auth middleware)
           |
     +-----+-----+-----+-----+
     v     v     v     v     v
  core  search registry auth  optional: bulk, SDC, terminology, sync hub
     |     |       |
     v     v       v
  store backends (sqlite / postgres TenantDB)
```

| Direction | Package | Relationship |
|-----------|---------|--------------|
| Downstream | **core** | `ResourceService` — CRUD, history, transaction/batch, patch |
| Downstream | **search** | `SearchService` — type search and POST `_search` |
| Downstream | **registry** | `CapabilitySource` — `/metadata` |
| Downstream | **auth** | `AuthChecker`, patient scope via `PatientReferenceResolver` |
| Downstream | **sync** | Optional `/sync/push`, `/sync/pull` when hub middleware wired |
| Optional | **export** / **bulkimport** | Bulk Data `$export` / `$import` services |
| Optional | **hooks** | Four-point SPI on `Config.Hooks` |

## When to use it

- Serving a FHIR REST API from a Go binary or sidecar
- Integration tests that exercise end-to-end JSON translation over real stores
- Wiring SMART, API-key, or service-account auth via `PrincipalResolver` without changing core/search APIs
- Exposing terminology, SDC, measure evaluation, or bulk operations through optional service interfaces

## Import alias

This package is named `http` and shadows the standard library. Always import with an alias:

```go
import (
    nethttp "net/http"

    hahttp "github.com/degoke/haistack/pkg/http"
)
```

## Usage modes

### 1. Minimal handler (CRUD only)

```go
handler, err := hahttp.NewHandler(hahttp.Config{
    ResourceService: hahttp.CoreResourceService{Svc: coreSvc},
})
if err != nil {
    log.Fatal(err)
}
nethttp.ListenAndServe(":8080", handler)
```

Without `SearchService` or `CapabilitySource`, type-level search and `/metadata` return not-supported outcomes.

### 2. Full stack (CRUD + search + metadata + auth)

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
        return principal, tenant, nil
    },
    AuthChecker: hahttp.PolicyAuthChecker{Engine: authEngine},
})
```

When `PrincipalResolver` and `AuthChecker` are both set, built-in middleware runs before handlers. Set `AuthMiddleware` instead for fully custom wrapping.

### 3. Bulk Data, SDC, and terminology extensions

Wire optional services on `Config`:

```go
handler, err := hahttp.NewHandler(hahttp.Config{
    ResourceService:        hahttp.CoreResourceService{Svc: coreSvc},
    BulkExportService:      exportSvc,
    BulkImportService:      importSvc,
    SDCService:             sdcAdapter,
    MeasureEvaluateService: measureSvc,
    TerminologyService:     terminologySvc,
    TerminologyScope:       tenantID,
    ValidateService:        validateAdapter,
})
```

When a service is nil, related routes return FHIR `OperationOutcome` with not-supported or not-implemented semantics.

### 4. Sync hub routes with authentication

```go
root, err := hahttp.NewRootHandlerWithSyncMiddleware(hahttp.RootConfig{
    FHIR:           fhirHandler,
    Hub:            scopedHub, // implements sync.ScopedHubServer
    SyncMiddleware: authMiddleware,
})
```

Expose `POST /sync/push` and `GET /sync/pull`. Pull defaults to 100 events; limit query accepts 1–1000.

### 5. Custom service implementations (mocks and alternate backends)

Implement narrow interfaces directly:

```go
type myResources struct{}

func (m myResources) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) { ... }
func (m myResources) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) { ... }
// Update, Delete, History, ProcessTransactionBundle, ProcessBatchBundle, Patch
```

### 6. Rate limiting and hooks

```go
handler, err := hahttp.NewHandler(hahttp.Config{
    ResourceService: hahttp.CoreResourceService{Svc: coreSvc},
    RateLimit: hahttp.RateLimitConfig{
        Requests: 1000,
        Window:   time.Minute,
    },
    Hooks: myHooks, // implements hooks.Hooks
})
```

Process-local limiter emits 429 with `Retry-After`; use a shared edge limiter for multi-instance deployments.

## Examples

**Patient compartment read:**

```http
GET /fhir/Patient/{id}/$everything
Accept: application/fhir+json
```

**Conditional update:**

```http
PUT /fhir/Patient?identifier=system|value
If-Match: W/"version-id"
```

**Async bulk export kickoff:**

```http
GET /fhir/Patient/$export
Prefer: respond-async
```

Returns 202 with `Content-Location` when `BulkExportService` is configured.

**SMART scope filter enforcement:**

```go
handler, err := hahttp.NewHandler(hahttp.Config{
    AuthBundleResolver: smartResolver,
    ScopeFilterMatcher: smart.DefaultScopeFilterMatcher(),
    // PrincipalResolver + AuthChecker …
})
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
| `PATCH` | `/fhir/{ResourceType}/{id}` | JSON Patch or FHIR Patch | 200 + resource |
| `DELETE` | `/fhir/{ResourceType}/{id}` | Delete resource | 204 No Content |
| `GET` | `/fhir/{ResourceType}/{id}/_history` | Instance history (`_since`, `_at`) | 200 + history Bundle |
| `GET` | `/fhir/{ResourceType}/{id}/_history/{vid}` | vread | 200 + resource, 410 if deleted |
| `GET` | `/fhir/Patient/{id}/$everything` | Patient compartment bundle | 200 + searchset Bundle |
| Bulk `$export` / `$import` | various | Async bulk data | 202 / 200 / 404 |
| Measure `$evaluate-measure` | type and instance | CQF evaluation | 200 + MeasureReport |
| SDC on Questionnaire / QuestionnaireResponse | `$populate`, `$assemble`, … | SDC adapter | 200 or OperationOutcome |
| `POST` / `GET` | `/sync/push`, `/sync/pull` | Sync hub (optional middleware) | 200 |

JSON projections (`_summary`, `_elements`) and JSON/XML content negotiation are supported. Unsupported `Accept` or `_format` values return 406.

### Deferred / requires wiring

- Full CapabilityStatement conformance coverage for every optional service
- SMART metadata and built-in token runtime (use `pkg/smart` + resolver)
- Services nil → route-level not-supported responses

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
| `AuthChecker` | no | nil | Read/write/search/export authorization |
| `AuthBundleResolver` | no | nil | SMART 2.2 scope filter context |
| `PatientReferenceResolver` | no | nil | Patient-scoped read/search enforcement |
| `SDCService` | no | nil | SDC operations |
| `MeasureEvaluateService` | no | nil | Measure/$evaluate-measure |
| `BulkExportService` / `BulkImportService` | no | nil | Bulk Data |
| `TerminologyService` | no | nil | `$lookup`, `$expand`, `$validate-code` |
| `OperationService` | no | nil | Custom `$operation` |
| `RateLimit` | no | disabled | Process-local fixed-window limiter |
| `Hooks` | no | nil | Incoming/outgoing HTTP + core storage hooks |

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
- **Export** → `CanBulkExport` when bulk export is configured

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
| `sync.go` | Sync root handler and middleware |
| `bulk.go` / `bulk_import.go` | Bulk Data routes |

## Where it fits

| Package | Role |
|---------|------|
| **http** | Transport adapter (this package) |
| **core** | Resource lifecycle |
| **search** | Search execution |
| **registry** | Capability metadata |
| **auth** / **smart** | Authorization |
| **sync** | Optional hub protocol over HTTP |
| **runtime** | Typical wiring of all services into `Config` |

## Limits

- No built-in OAuth authorization server — integrate `pkg/oauth` / `pkg/smart` externally
- Rate limit is process-local unless supplemented at the gateway
- CapabilityStatement reflects wired services; unwired operations appear absent or not-supported at runtime
- XML support covers negotiated read/write paths; not every auxiliary admin route may expose XML
- Sync routes require explicit middleware and registered hub nodes for Postgres tenants

## Related docs

- [docs/architecture.md](../../docs/architecture.md) — API layer placement
- [pkg/core/README.md](../core/README.md) — resource service delegated by HTTP
- [pkg/search/README.md](../search/README.md) — search adapter
- [pkg/registry/README.md](../registry/README.md) — capability snapshot
- [pkg/auth/README.md](../auth/README.md) — policy engine
- [pkg/smart/README.md](../smart/README.md) — SMART scopes and auth bundle
- [pkg/sync/README.md](../sync/README.md) — hub protocol behind sync routes
- [doc.go](./doc.go) — endpoint list and design principles

## Testing

```bash
go test ./pkg/http/... -count=1
```

- **Unit tests** (`http_test.go`) — mock services for endpoints, auth paths, and error mapping
- **Integration tests** (`integration_test.go`) — sqlite-backed core + search wired through adapters
