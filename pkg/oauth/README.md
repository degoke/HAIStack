# haistack-oauth (`pkg/oauth`)

Optional built-in OAuth 2.0 / SMART-on-FHIR **authorization server** for self-contained HAIStack deployments (edge, demos, integration tests, air-gapped environments).

External IdPs (Keycloak, Auth0, Epic, Cerner) remain fully supported — omit this package and wire token validation through `pkg/smart` + `pkg/http` as before.

## What it does

| Component | Role |
|-----------|------|
| `Server` | Authorization + token + revoke endpoints |
| `ClientRegistry` | Static SMART client metadata |
| `WireHTTP` | OAuth routes + `PrincipalResolver` for FHIR auth |
| `ScopePolicyAuthChecker` | SMART tokens → `pkg/auth` policy |
| `store` | SQLite-backed auth codes, refresh tokens, revocation |

## Endpoints

| Path | Description |
|------|-------------|
| `GET/POST /oauth/authorize` | Authorization code + PKCE (`S256`) |
| `POST /oauth/token` | `authorization_code`, `refresh_token`, `client_credentials` |
| `POST /oauth/revoke` | Revoke access or refresh tokens |
| `GET /.well-known/smart-configuration` | SMART discovery |
| `GET /.well-known/openid-configuration` | OIDC subset discovery |
| `GET /.well-known/jwks.json` | JWKS (RS256 signers) |

## Quick start

```go
reg := oauth.NewClientRegistry()
_ = reg.Register(oauth.Client{
    ClientRegistration: smart.ClientRegistration{
        ClientID:     "demo-app",
        RedirectURIs: []string{"https://app.example/callback"},
        Scopes:       []string{"openid", "offline_access", "patient/Patient.read"},
    },
    DefaultPatient: "patient-123",
    TenantHint:     "tenant-a",
})

srv, _ := oauth.NewServer(oauth.Config{
    Issuer:      "https://fhir.example.com",
    FHIRBaseURL: "https://fhir.example.com/fhir",
    Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "server-1"},
    Clients:     reg,
})

wired, _ := oauth.WireHTTP(oauth.WireConfig{
    Server: srv,
    Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
        DefaultTenantID:  "tenant-a",
        DefaultUserRoles: []string{"clinician"},
    }),
})

// Mount wired.OAuthHandler alongside FHIR; use wired.PrincipalResolver with pkg/http.
```

## SQLite persistence

Run `pkg/sqlite` migration `0012_oauth.sql`, then:

```go
stores, _ := store.NewSQLiteStores(db.SQL())
srv, _ := oauth.NewServer(oauth.Config{
    // ...
    CodeStore:       stores.Codes,
    RefreshStore:    stores.Refresh,
    RevocationStore: stores.Revocation,
})
```

## Security notes

- PKCE required for public clients; `plain` is rejected
- Redirect URIs must match exactly
- Auth codes are short-lived and single-use
- Refresh tokens rotate by default
- Revoked access token JTIs are rejected by `WireHTTP`/`BearerPrincipalResolver`

See [examples/oauth-fhir-server](../../examples/oauth-fhir-server/main.go) for a runnable demo.
