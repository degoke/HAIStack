# haistack-smart (`pkg/smart`)

Optional SMART on FHIR library for Health AI Stack. It parses scopes, validates
tokens/assertions, and adapts SMART launch context into `pkg/auth` — without
owning OAuth servers, EHR launch runtimes, or offline MVP paths.

## What it does

v1 centers on interpretation and adaptation:

- `ParseScopes` / `ScopeSet` — normalize and match `patient|user|system` scopes
- `LaunchContext` / `BuildLaunchContext` — patient, encounter, user, tenant hints
- `TokenValidator` / `ValidateToken` — JWT structure, iss/aud/exp/nbf, scope extraction
- `BackendServiceAuth` / `ValidateBackendAssertion` — signed backend client assertions
- `AuthAdapter` — translate SMART inputs into `pkg/auth` principals and requests
- `ClientRegistration` — minimal static client metadata for later expansion

Explicitly out of v1: EHR/standalone launch orchestration, dynamic client
registration, refresh-token lifecycle, SMART UI/session management, and HTTP
middleware as the package center.

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

Normalized resource scopes:

| Pattern | Meaning |
|---------|---------|
| `patient/*.read` | Patient-compartment read of any resource |
| `patient/{Resource}.read` | Patient-compartment read of one type |
| `user/*.read` / `user/*.write` | User-level read/write |
| `user/{Resource}.read` / `.write` | User-level typed access |
| `system/*.read` / `system/*.write` | Backend service system access |

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
