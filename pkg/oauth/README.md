# haistack-oauth (`pkg/oauth`)

Optional built-in OAuth 2.0 / SMART-on-FHIR **authorization server** for self-contained HAIStack deployments (edge, demos, integration tests, air-gapped environments).

External IdPs (Keycloak, Auth0, Epic, Cerner) remain fully supported — omit this package and wire token validation through `pkg/smart` + `pkg/http` as before.

## What it does

| Component | Role |
|-----------|------|
| `Server` | Authorization + token + revoke + launch endpoints |
| `ClientRegistry` | Static SMART client metadata |
| `WireHTTP` | OAuth routes + `PrincipalResolver` for FHIR auth |
| `ScopePolicyAuthChecker` | SMART scope checks + `pkg/auth` policy |
| `ApplySQLiteStores` | SQLite-backed auth codes, launch tokens, refresh tokens, revocation |
| `store` | Low-level SQLite store implementations |

## Endpoints

| Path | Description |
|------|-------------|
| `GET/POST /oauth/authorize` | Authorization code + PKCE (`S256`); CSRF-protected consent when `AutoApprove` is false |
| `POST /oauth/launch` | Issue single-use EHR launch tokens (`launch_issuer_id` + `launch_issuer_secret`) |
| `POST /oauth/token` | `authorization_code`, `refresh_token`, `client_credentials` |
| `POST /oauth/revoke` | Revoke access or refresh tokens (confidential client auth required) |
| `POST /oauth/introspect` | RFC 7662 token introspection (confidential client auth required) |
| `POST /oauth/register` | Dynamic client registration (RFC 7591 subset; enabled by default) |
| `GET /.well-known/smart-configuration` | SMART discovery (also mirrored at `{fhir_base}/.well-known/...` by `pkg/http`) |
| `GET /.well-known/openid-configuration` | OIDC subset discovery |
| `GET /.well-known/jwks.json` | JWKS (RS256 signers) |

## Quick start

```go
reg := oauth.NewClientRegistry()
_ = reg.Register(oauth.Client{
    ClientRegistration: smart.ClientRegistration{
        ClientID:     "demo-app",
        RedirectURIs: []string{"https://app.example/callback"},
        Scopes:       []string{"openid", "offline_access", "patient/Patient.read", "launch/patient"},
    },
    DefaultPatient: "patient-123",
    TenantHint:     "tenant-a",
})

cfg := oauth.Config{
    Issuer:      "https://fhir.example.com",
    FHIRBaseURL: "https://fhir.example.com/fhir",
    Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "server-1"},
    Clients:     reg,
}
_ = store.ApplySQLiteStores(&cfg, db.SQL()) // recommended for production-like hosts
srv, _ := oauth.NewServer(cfg)

wired, _ := oauth.WireHTTP(oauth.WireConfig{
    Server: srv,
    Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
        DefaultTenantID:  "tenant-a",
        DefaultUserRoles: []string{"clinician"},
    }),
})
fhirHandler, _ := hahttp.NewHandler(hahttp.Config{
    PrincipalResolver: wired.PrincipalResolver,
    AuthChecker:       wired.ScopePolicyAuthChecker(authEngine),
})
```

## Consent UI

When `AutoApprove` is false (the default), authorize requests render a consent page with:

- configurable title, logo, colors, and footer text (`ConsentUI`)
- registered client display name when `ClientName` is set
- server-side consent session + CSRF token (HttpOnly cookie scoped to the authorize path)
- optional resource-owner login via `ConsentLogin` before consent is shown

Demos and tests opt into silent authorization explicitly:

```go
autoApprove := true
cfg.AutoApprove = &autoApprove
```

Optional login hook for hosts that authenticate users before consent:

```go
cfg.ConsentLogin = myLoginHandler // implements oauth.ConsentLoginHandler
```

When `ConsentLogin` returns a subject, that identity is written into issued tokens (unless an EHR launch context already supplied a user). Consent sessions are persisted to SQLite when using `ApplySQLiteStores` (issuer-scoped, single-use, 5-minute TTL). See [OPERATIONS.md](OPERATIONS.md) for scheduled cleanup and production vs Inferno reference configuration.

## Dynamic client registration

`POST /oauth/register` accepts a JSON client metadata document and returns a server-assigned `client_id` (and `client_secret` for confidential clients). Callers must not supply `client_id`; existing clients cannot be overwritten. The endpoint is advertised as `registration_endpoint` in SMART discovery when enabled (default). Clients are persisted when `ApplySQLiteStores` wires `ClientStore`. Client secrets are stored as bcrypt hashes in SQLite.

Gate open registration in production with a bearer token:

```go
cfg.RegistrationAccessToken = os.Getenv("OAUTH_REGISTRATION_TOKEN")
if err := oauth.ApplyProductionDefaults(&cfg); err != nil {
    return err
}
```

`ApplyProductionDefaults` requires `RegistrationAccessToken` when dynamic registration is enabled. Redirect URIs must use **https** except for loopback **http** hosts (`localhost`, `127.0.0.1`, `::1`). Server-assigned client IDs retry on the extremely unlikely collision with an existing registration.

```bash
curl -X POST "$BASE/oauth/register" \
  -H 'Authorization: Bearer change-me-registration-token' \
  -H 'Content-Type: application/json' \
  -d '{"client_name":"My App","redirect_uris":["https://app.example/callback"],"token_endpoint_auth_method":"none"}'
```

Confidential clients may authenticate at the token endpoint with `client_secret_basic` (HTTP Basic) or `client_secret_post` (form field).

Disable dynamic registration explicitly when needed:

```go
disabled := false
cfg.DynamicClientRegistration = &disabled
```
The built-in consent page is intentionally minimal (HTML + CSRF cookie). `ConsentUI.LogoURL` accepts **http/https** URLs only. Edge and demo IdPs can rely on title/logo theming without a full login portal; production hosts typically integrate an external IdP or custom `ConsentLogin` implementation.

## EHR launch issuer credentials

Configure separate launch issuer credentials (distinct from SMART app client credentials):

```go
// Single static credential (simple deployments)
cfg.LaunchIssuerAuth = &oauth.LaunchIssuerAuth{
    ClientID:     "ehr-launcher",
    ClientSecret: "change-me",
}

// Rotating credentials (old + new secrets both valid during rotation)
// Secrets are bcrypt-hashed at rest inside LaunchIssuerRegistry.
issuers := oauth.NewLaunchIssuerRegistry()
_ = issuers.RegisterRotating("ehr-launcher", "old-secret", "new-secret")
cfg.LaunchIssuers = issuers

// Optional mTLS for POST /oauth/launch (requires TLS termination with client certs)
cfg.LaunchIssuerMTLS = &oauth.LaunchIssuerMTLSConfig{
    RequireMTLS:        true,
    AllowedCommonNames: []string{"ehr-launcher.example.com"},
}
// When RequireMTLS is true, a verified client certificate alone satisfies launch auth.
// When AllowedCommonNames is non-empty, any presented client certificate must match.
```

An EHR posts launch context with those credentials (unless using mTLS-only auth):

```bash
curl -X POST "$BASE/oauth/launch" \
  -d launch_issuer_id=ehr-launcher \
  -d launch_issuer_secret=change-me \
  -d patient=patient-123
# -> {"launch":"<single-use-token>"}
```

The app then opens `/oauth/authorize?...&launch=<token>`.

## Multi-tenant issuers

Register per-tenant issuer settings and mount tenant-scoped routes under `/t/{tenantId}/`:

```go
tenants := oauth.NewTenantRegistry()
_ = tenants.Register(oauth.TenantIssuerConfig{
    TenantID:    "tenant-a",
    Issuer:      "https://fhir.example.com/t/tenant-a",
    FHIRBaseURL: "https://fhir.example.com/t/tenant-a/fhir",
    ConsentUI: &oauth.ConsentUIConfig{
        Title:        "Tenant A Authorization",
        PrimaryColor: "#0f766e",
    },
    LaunchIssuers: tenantAIssuers, // required for /oauth/launch in multi-tenant mode
})

mts, _ := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
    Base: oauth.Config{
        Signer:  oauth.RS256Signer{PrivateKey: key, Kid: "server-1"},
        Clients: reg,
    },
    Tenants: tenants,
})
wired, _ := oauth.WireMultiTenantHTTP(oauth.MultiTenantConfig{Base: base, Tenants: tenants}, adapter)
// wired.OAuthHandler serves /t/tenant-a/oauth/* and discovery documents per tenant
```

Multi-tenant servers **do not inherit** base `LaunchIssuerAuth` / `LaunchIssuers`; register launch credentials on each `TenantIssuerConfig` that needs EHR launch. Authorization codes, launch tokens, and refresh tokens are bound to the issuing tenant's OAuth issuer URL and cannot be consumed at another tenant's endpoints.

## SQLite persistence

**Auth codes, refresh tokens, launch tokens, and revocation default to in-memory stores** when `store.ApplySQLiteStores` is not used. This is fine for unit tests and zero-config demos, but **production-like hosts should wire SQLite** (or another shared store) before calling `NewServer`:

```go
if oauth.UsesInMemoryStores(cfg) {
    log.Println("warning: OAuth persistence is in-memory only")
}
```

Run `pkg/sqlite` migrations `0012_oauth.sql` through `0018_oauth_client.sql`, then:

```go
cfg := oauth.Config{ /* issuer, signer, clients, ... */ }
_ = store.ApplySQLiteStores(&cfg, db.SQL())
cfg.RegistrationAccessToken = os.Getenv("OAUTH_REGISTRATION_TOKEN")
_ = oauth.ApplyProductionDefaults(&cfg) // recommended for production-like hosts
srv, _ := oauth.NewServer(cfg)
```

Expired consent sessions are purged opportunistically on create/consume and via a background sweeper:

```go
consentCtx, stopConsent := context.WithCancel(ctx)
defer stopConsent()
oauth.StartConsentSessionCleanup(consentCtx, cfg.ConsentSessionStore, oauth.DefaultConsentSessionCleanupInterval)
```

See [OPERATIONS.md](OPERATIONS.md) for the full production vs Inferno reference checklist.

## Security notes

- `AutoApprove` defaults to **false**; production hosts should keep interactive consent enabled
- PKCE required for public clients; `plain` is rejected
- Redirect URIs must match exactly
- Auth codes, launch tokens, and refresh tokens must match the issuing OAuth issuer at exchange/consume/refresh time
- Auth codes, launch tokens, and refresh tokens are short-lived and single-use (SQLite-backed when using `ApplySQLiteStores`; in-memory otherwise)
- Auth codes, launch tokens, refresh tokens, consent sessions, and registered clients use SQLite when `ApplySQLiteStores` is wired
- Client secrets and launch issuer secrets are compared with constant-time equality checks; persisted client secrets and launch issuer registry secrets are bcrypt-hashed at rest
- Multi-tenant servers overlay static tenant clients on the shared SQLite `ClientStore` so dynamic registration remains issuer-scoped
- `registration_endpoint` is advertised in SMART and OIDC discovery when dynamic registration is enabled
- Consent sessions (CSRF state) are single-process in-memory unless SQLite is wired
- Refresh tokens rotate by default
- `/oauth/introspect` and `/oauth/revoke` require **confidential** registered client authentication
- `WireHTTP` exposes `ScopePolicyAuthChecker(engine)` wired to the same SMART adapter
- Revoked access token JTIs are rejected by `WireHTTP`/`BearerPrincipalResolver`
- `ScopePolicyAuthChecker` enforces SMART scopes before policy evaluation and denies access when no policy engine is configured

See [examples/oauth-fhir-server](../../examples/oauth-fhir-server/main.go) for a runnable demo.
