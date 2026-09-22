# haistack-smart (`pkg/smart`)

Optional **SMART on FHIR** library for HAIStack: scope parsing, token and backend-assertion validation, launch context, and adaptation into [`pkg/auth`](../auth/README.md). It does not replace an OAuth authorization server or EHR launch UI.

## What it does

`pkg/smart` (haistack-smart) interprets SMART scopes and JWT claims and produces **`auth`-ready inputs**—principals, tenant context, permissions, and request builders. **Authorization decisions stay in `pkg/auth`**; this package normalizes SMART semantics only.

v1 capabilities:

- **Scopes** — `ParseScopes`, `ScopeSet` matching (`Allows`, `AllowsRead`, `AllowsWrite`) for SMART 1.x `.read`/`.write` patterns and SMART 2.2 **CRUDS** granular scopes with optional search-parameter filters (`patient/Observation.rs?category=laboratory`).
- **Launch context** — `BuildLaunchContext`, `ExtractLaunchContext`, `LaunchContextInput` for patient, encounter, user, tenant hints (no full EHR/standalone launch orchestration).
- **Tokens** — `TokenValidator`, `ValidateToken`, `ValidateClaims` for JWT structure, `iss`/`aud`/`exp`/`nbf`, scope extraction; optional `PEMVerifier` / custom signature verification.
- **Backend services** — `BackendServiceAuth`, `ValidateBackendAssertion` for signed client assertions, allow-listed clients, mandatory **`jti` replay protection**, strict assertion time checks.
- **Auth bridge** — `AuthAdapter`, `ToAuthRequests`, `FromBackendService`, `ToPatientScopeRequest` map SMART into `auth.Principal`, `auth.TenantContext`, and permission lists.
- **Metadata** — `Configuration`, `DefaultConfiguration(issuer)` for SMART App Launch discovery document fields (hosts serve via `pkg/oauth` or custom HTTP).
- **Scope filters** — `RegistryScopeFilterMatcherChain`, `InstallRegistryScopeFilterMatcher` integrate SearchParameter-aware filter matching with FHIRPath and search registry normalization.
- **Persistence helpers** — `NewFileBackendClientStore`, `NewFileReplayStore` for single-instance backend-service deployments (not multi-instance production defaults).

Explicitly **out of v1**: EHR/standalone launch product flows, refresh-token lifecycle, SMART session UI, dynamic client registration product surface, and HTTP middleware as the architectural center of the package.

## How it fits in the ecosystem

| Layer | Package | Role |
|-------|---------|------|
| OAuth AS / token endpoint | `oauth` (optional) | Codes, refresh tokens, client store in SQLite/Postgres |
| **SMART interpretation** | **`smart`** | Scopes, launch, JWT/assertion validation, auth adapter |
| Authorization | `auth` | Engine checks patient scope, read/write, tenant policy |
| FHIR API | `core`, `http` | Resource handlers; optional `ScopeFilterMatcher` wiring |
| Offline/local MVP | `core`, `sync`, `view`, `ai` | Do not require SMART |

Hosts own transport to token endpoints and AS configuration. SMART plugs in where you validate access tokens or backend assertions before calling `auth.Engine`.

Multi-instance SMART hosts should inject transactional `BackendClientStore` and `ReplayStore` implementations instead of JSON file stores.

## When to use it

Import `pkg/smart` when you:

- Accept **SMART scopes** on user or patient tokens and need deterministic matching before `auth` checks.
- Run **backend services** with JWT client assertions and need assertion validation + service principal mapping.
- Advertise **SMART configuration** (`permission-v2`, `permission-v2.2`, launch capabilities) consistently with what this library parses.
- Enforce **granular scope filters** on search/read using registered SearchParameters.

Skip this package for purely local/offline deployments with no OAuth, or when you implement SMART entirely in another language and only pass pre-mapped roles into `auth`.

## Usage modes

### 1. Parse and match scopes

```go
scopes, err := smart.ParseScopes("patient/*.read launch/patient openid")
ok := scopes.AllowsRead(smart.ActorPatient, "Observation")
denied := scopes.AllowsWrite(smart.ActorPatient, "Observation") // patient write not in v1 patterns
```

### 2. User/patient token → auth bundle

```go
launch := smart.BuildLaunchContext(smart.LaunchContextInput{
    PatientID:  "pat-1",
    UserID:     "Practitioner/pract-1",
    TenantHint: "tenant-a",
    Scopes:     scopes,
})
adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
    DefaultTenantID:  "tenant-a",
    DefaultUserRoles: []string{"smart-user"},
})
bundle, err := adapter.ToAuthRequests(claims, launch)
req := adapter.ToPatientScopeRequest(bundle, "")
// eng.CheckPatientScope(ctx, req)
```

### 3. Backend service assertion → service principal

```go
bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", smart.BackendClient{
    ClientID:       "backend-app",
    AllowedScopes:  []string{"system/*.read", "system/*.write"},
    Key: smart.ClientKeyMetadata{Algorithm: "RS256", PublicKeyPEM: pubPEM},
    TenantHint:     "tenant-svc",
})
claims, client, err := bsa.ValidateBackendAssertion(assertionJWT, smart.TokenValidateOptions{})
bundle, err := adapter.FromBackendService(claims, client, smart.LaunchContext{})
// bundle.Principal.Kind == auth.KindService
```

### 4. Token validation with optional PEM verifier

```go
tv := smart.NewTokenValidator(smart.TokenValidatorConfig{
    Issuer:   "https://auth.example",
    Audience: "https://fhir.example",
    Verifier: smart.NewPEMVerifier(pubPEM),
})
claims, err := tv.ValidateToken(rawJWT, smart.TokenValidateOptions{})
```

### 5. SMART discovery metadata

```go
doc := smart.DefaultConfiguration("https://auth.example")
// Override AuthorizationEndpoint, TokenEndpoint, ScopesSupported for your deployment
```

Capabilities include `permission-v2`, `permission-v2.2`, launch modes, and PKCE `S256`.

### 6. HTTP scope filter matcher (search/read enforcement)

Prefer per-handler wiring in `hahttp.Config`:

```go
ScopeFilterMatcher: smart.RegistryScopeFilterMatcherChain(searchRegistry, fhirpathEngine),
```

Process-wide default: `smart.InstallRegistryScopeFilterMatcher(...)`. Unregistered SearchParameters fall back to the MVP matcher.

### 7. File-backed backend client + replay store (single node)

```go
clientStore, err := smart.NewFileBackendClientStore("/etc/haistack/backend-clients.json")
replayStore, err := smart.NewFileReplayStore("/var/lib/haistack/jti-replay.json")
bsa, err := smart.NewBackendServiceAuth(tokenURL, client, smart.WithReplayStore(replayStore))
```

## Examples (from tests and public API)

**SMART 1.x patterns** (`TestParseScopes_ValidPatterns`):

```go
smart.ParseScopes("patient/*.read")
smart.ParseScopes("patient/Observation.read")
smart.ParseScopes("user/*.write")
smart.ParseScopes("system/*.read")
```

**Launch and specialty tokens** (`TestParseScopes_LaunchAndSpecialty`):

```go
set, _ := smart.ParseScopes("launch launch/patient openid fhirUser patient/*.read")
set.HasLaunch() // true
```

**Malformed scopes** (`TestParseScopes_Malformed`):

```go
_, err := smart.ParseScopes("patient/")
errors.Is(err, smart.ErrInvalidScope) // true for patient/, patient.read, patient/*.write, etc.
```

**Duplicate collapse** (`TestParseScopes_NormalizeDuplicatesAndOverlaps`):

```go
set, _ := smart.ParseScopes("patient/*.read patient/*.read patient/Observation.read")
// Len() == 1, raw scope patient/*.read
```

**ScopeSet matching** (`TestScopeSet_Matching`):

```go
set, _ := smart.ParseScopes("user/Patient.read user/*.write system/*.read")
set.AllowsRead(smart.ActorUser, "Patient")        // true
set.AllowsWrite(smart.ActorUser, "Patient")       // true via user/*.write
set.AllowsRead(smart.ActorSystem, "Observation")  // true
set.AllowsWrite(smart.ActorSystem, "Observation") // false
```

**BuildLaunchContext** merges claims and input (`TestBuildLaunchContext` in `smart_test.go`).

**SMARTVersion constant** — documents implemented scope patterns:

```go
const smart.SMARTVersion = "2.2 granular scopes (CRUDS + search-parameter filters); v1 read/write patterns supported"
```

## Configuration / key types

| Type | Purpose |
|------|---------|
| `ScopeSet`, `Scope`, `ActorClass`, `AccessVerb` | Parsed scope model |
| `ParseScopes`, `ScopeParser` | Entry parsing |
| `LaunchContext`, `LaunchContextInput`, `BuildLaunchContext` | Launch hints |
| `TokenClaims`, `TokenValidator`, `TokenValidateOptions` | JWT validation |
| `BackendServiceAuth`, `BackendClient`, `ClientKeyMetadata` | Backend assertions |
| `BackendClientStore`, `ReplayStore` | Client allow-list and jti replay |
| `AuthAdapter`, `AuthAdapterConfig`, `AuthBundle` | SMART → auth translation |
| `Configuration`, `DefaultConfiguration` | SMART metadata document |
| `ClientRegistration` | Minimal static client metadata |
| `ErrInvalidScope`, `ErrClientNotAllowed`, … | Error taxonomy |

Backend assertion scopes must be a **subset** of the client's `AllowedScopes`. Unknown client IDs fail closed.

## Where it fits

```text
Client app / backend job
        │
        ▼
   OAuth AS (host: pkg/oauth or external)
        │
        ▼
   Access token or client assertion JWT
        │
        ▼
   pkg/smart  (parse scopes, validate token/assertion, build LaunchContext)
        │
        ▼
   AuthAdapter → AuthBundle
        │
        ▼
   pkg/auth.Engine  (CheckPatientScope, read/write decisions)
        │
        ▼
   pkg/core / handlers (optional ScopeFilterMatcher on search)
```

## Limits

- **Deny by absence** — Scopes must explicitly allow resource and verb; broad hospital policy may further narrow access in `auth`.
- **Patient scopes** — Only `r` and `s` CRUDS letters; `.write` on patient compartment is rejected at parse time in v1 tests.
- **Launch flows** — No built-in EHR iframe launch, standalone redirect choreography, or refresh rotation.
- **Signature verification** — Optional; hosts must configure verifiers for production token trust.
- **File stores** — Convenient for dev/single node; race and durability limits for clustered deployments.
- **Filter matching** — Requires SearchParameter registration for full FHIRPath filter semantics; otherwise MVP fallback applies.
- **OAuth persistence** — Authorization codes and refresh tokens live in `pkg/oauth/store`, not in `pkg/smart`.

## Related docs

- [doc.go](./doc.go) — API inventory and non-goals
- [pkg/auth/README.md](../auth/README.md)
- [pkg/oauth](../oauth) — authorization server persistence and endpoints
- Root README SMART / security sections where present
