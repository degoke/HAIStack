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
| `GET/POST /oauth/authorize` | Authorization code + PKCE (`S256`); interactive consent when `AutoApprove` is false |
| `POST /oauth/launch` | Issue single-use EHR launch tokens (client auth required) |
| `POST /oauth/token` | `authorization_code`, `refresh_token`, `client_credentials` |
| `POST /oauth/revoke` | Revoke access or refresh tokens (client auth required) |
| `POST /oauth/introspect` | RFC 7662 token introspection (client auth required) |
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

// Mount wired.OAuthHandler alongside FHIR; use wired.PrincipalResolver with pkg/http.
// ScopePolicyAuthChecker should include the same AuthAdapter used by WireHTTP.
```

For demos and tests only, opt into silent authorization:

```go
autoApprove := true
cfg.AutoApprove = &autoApprove
```

## EHR launch

An EHR (or simulator) creates a launch context, then the app passes the returned token to `/oauth/authorize?launch=...`:

```go
// POST /oauth/launch (client_id required)
// patient=...&encounter=...&user=...
// -> {"launch":"<single-use-token>"}
```

The authorize endpoint consumes the launch token and binds patient/encounter/user context into the issued tokens.

## Multi-tenant issuers

Register per-tenant issuer settings and mount tenant-scoped routes under `/t/{tenantId}/`:

```go
tenants := oauth.NewTenantRegistry()
_ = tenants.Register(oauth.TenantIssuerConfig{
    TenantID:    "tenant-a",
    Issuer:      "https://fhir.example.com/t/tenant-a",
    FHIRBaseURL: "https://fhir.example.com/t/tenant-a/fhir",
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
- Auth codes, launch tokens, and refresh tokens are short-lived and single-use (SQLite-backed when using `ApplySQLiteStores`)
- Refresh tokens rotate by default
- `/oauth/introspect` and `/oauth/revoke` require registered client authentication
- Revoked access token JTIs are rejected by `WireHTTP`/`BearerPrincipalResolver`
- `ScopePolicyAuthChecker` enforces SMART scopes before policy evaluation and denies access when no policy engine is configured

See [examples/oauth-fhir-server](../../examples/oauth-fhir-server/main.go) for a runnable demo.
