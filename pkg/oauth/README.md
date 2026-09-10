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

The built-in consent page is intentionally minimal (HTML + CSRF cookie). Edge and demo IdPs can rely on title/logo theming without a full login portal; production hosts typically integrate an external IdP or custom `ConsentLogin` implementation.

## EHR launch issuer credentials

Configure separate launch issuer credentials (distinct from SMART app client credentials):

```go
// Single static credential (simple deployments)
cfg.LaunchIssuerAuth = &oauth.LaunchIssuerAuth{
    ClientID:     "ehr-launcher",
    ClientSecret: "change-me",
}

// Rotating credentials (old + new secrets both valid during rotation)
issuers := oauth.NewLaunchIssuerRegistry()
_ = issuers.RegisterRotating("ehr-launcher", "old-secret", "new-secret")
cfg.LaunchIssuers = issuers

// Optional mTLS for POST /oauth/launch (requires TLS termination with client certs)
cfg.LaunchIssuerMTLS = &oauth.LaunchIssuerMTLSConfig{
    RequireMTLS:        true,
    AllowedCommonNames: []string{"ehr-launcher.example.com"},
}
```

An EHR posts launch context with those credentials:

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
    LaunchIssuers: tenantAIssuers, // optional per-tenant launch credentials
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

## SQLite persistence

**Auth codes, refresh tokens, launch tokens, and revocation default to in-memory stores** when `store.ApplySQLiteStores` is not used. This is fine for unit tests and zero-config demos, but **production-like hosts should wire SQLite** (or another shared store) before calling `NewServer`:

```go
if oauth.UsesInMemoryStores(cfg) {
    log.Println("warning: OAuth persistence is in-memory only")
}
```

Run `pkg/sqlite` migrations `0012_oauth.sql` and `0013_oauth_launch.sql`, then:

```go
cfg := oauth.Config{ /* issuer, signer, clients, ... */ }
_ = store.ApplySQLiteStores(&cfg, db.SQL())
srv, _ := oauth.NewServer(cfg)
```

## Security notes

- `AutoApprove` defaults to **false**; production hosts should keep interactive consent enabled
- PKCE required for public clients; `plain` is rejected
- Redirect URIs must match exactly
- Auth codes, launch tokens, and refresh tokens are short-lived and single-use (SQLite-backed when using `ApplySQLiteStores`; in-memory otherwise)
- Refresh tokens rotate by default
- `/oauth/introspect` and `/oauth/revoke` require **confidential** registered client authentication
- `WireHTTP` exposes `ScopePolicyAuthChecker(engine)` wired to the same SMART adapter
- Revoked access token JTIs are rejected by `WireHTTP`/`BearerPrincipalResolver`
- `ScopePolicyAuthChecker` enforces SMART scopes before policy evaluation and denies access when no policy engine is configured

See [examples/oauth-fhir-server](../../examples/oauth-fhir-server/main.go) for a runnable demo.
