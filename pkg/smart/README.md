# haistack-smart (`pkg/smart`)

Optional SMART on FHIR library for Health AI Stack. It parses scopes, validates
tokens/assertions, and adapts SMART launch context into `pkg/auth` — without
owning OAuth servers, EHR launch runtimes, or offline MVP paths.

## What it does

v1 centers on interpretation and adaptation:

- `ParseScopes` / `ScopeSet` — normalize and match `patient|user|system` scopes
- `LaunchContext` / `BuildLaunchContext` — patient, encounter, user, tenant hints
- `TokenValidator` / `ValidateToken` — JWT structure, iss/aud/exp/nbf, scope extraction
- `BackendServiceAuth` / `ValidateBackendAssertion` — signed backend client assertions,
  mandatory `jti` replay protection, and strict assertion time checks
- `AuthAdapter` — translate SMART inputs into `pkg/auth` principals and requests
- `ClientRegistration` — minimal static client metadata for later expansion

Hosts may use `NewFileBackendClientStore` and `NewFileReplayStore` for persisted
single-instance deployments, or inject shared transactional implementations via
`BackendClientStore` and `ReplayStore` for multi-instance deployments.

Explicitly out of v1: EHR/standalone launch orchestration, refresh-token
lifecycle, SMART UI/session management, and HTTP middleware as the package center.

## Usage

**Parse and match scopes:**

```go
scopes, err := smart.ParseScopes("patient/*.read launch/patient openid")
ok := scopes.AllowsRead(smart.ActorPatient, "Observation")
```

**Build launch context and adapt into auth:**

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
// eng.CheckPatientScope(ctx, req) — decisions stay in pkg/auth
```

**Backend service assertion:**

```go
bsa, err := smart.NewBackendServiceAuth("https://auth.example/token", smart.BackendClient{
    ClientID:      "backend-app",
    AllowedScopes: []string{"system/*.read", "system/*.write"},
    Key: smart.ClientKeyMetadata{
        Algorithm:    "RS256",
        PublicKeyPEM: pubPEM,
    },
    TenantHint: "tenant-svc",
})

claims, client, err := bsa.ValidateBackendAssertion(assertionJWT, smart.TokenValidateOptions{})
bundle, err := adapter.FromBackendService(claims, client, smart.LaunchContext{})
// bundle.Principal.Kind == auth.KindService
```

## Scope support

### SMART 1.x patterns (supported)

| Pattern | Meaning |
|---------|---------|
| `patient/*.read` | Patient-compartment read/search of any resource |
| `patient/{Resource}.read` | Patient-compartment read/search of one type |
| `user/*.read` / `user/*.write` | User-level read/search or create/update/delete |
| `user/{Resource}.read` / `.write` | User-level typed access |
| `system/*.read` / `system/*.write` | Backend service system access |

v1 `.read` maps to CRUDS `rs`; `.write` maps to `cud`; `.*` maps to `cruds`.

### SMART 2.2 granular scopes

| Pattern | Meaning |
|---------|---------|
| `patient/Observation.rs` | Patient read + search for Observations |
| `user/Patient.cruds` | User full CRUDS on Patient |
| `patient/Observation.rs?category=laboratory` | Read/search with search-parameter filter |

CRUDS letters: `r` read, `s` search, `c` create, `u` update, `d` delete.
Patient scopes only allow `r` and `s`.

Hosts advertise `permission-v2` and `permission-v2.2` via `DefaultConfiguration`.
Scope filters are enforced on search (query intersection), read/history (resource
check), and bundle post-filtering. Underlying `pkg/auth` policy may still narrow
apparently valid scopes.

**Filter matching:** Prefer per-handler wiring via `hahttp.Config.ScopeFilterMatcher`:

```go
ScopeFilterMatcher: smart.RegistryScopeFilterMatcherChain(searchRegistry, fhirpathEngine),
```

`smart.InstallRegistryScopeFilterMatcher` remains available for process-wide defaults.
Registered SearchParameters use FHIRPath extraction and search index normalization.
Unregistered parameters fall back to the MVP matcher.

Also parsed as metadata: `launch`, `launch/patient`, `launch/encounter`, and
specialty tokens such as `openid`, `fhirUser`, `offline_access`.

Malformed scopes return `ErrInvalidScope`. Duplicates and overlaps collapse
(e.g. `patient/Observation.read` under `patient/*.read`).

## Safety boundaries

- Deny-by-absence matching: scopes must explicitly allow a resource/verb
- Backend clients are allow-listed; unknown client ids fail
- Assertion scopes must be a subset of the client's `AllowedScopes`
- Signature verification is optional and pluggable (`PEMVerifier` or custom)
- No required HTTP server, browser session, or local MVP dependency

## Where it fits

| Layer | Role |
|-------|------|
| **smart** | SMART scope/token interpreter + auth adapter (this package) |
| **auth** | Decision engine; receives adapted principals and tenant/patient scope |
| **ai / view / sync / core** | Unchanged; SMART is optional and omitable |

See [doc.go](./doc.go) for API detail and non-goals.
