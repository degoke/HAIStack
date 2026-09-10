# pkg/oauth

Built-in OAuth2/OIDC authorization server for SMART on FHIR development and conformance smoke tests.

## Endpoints

| Path | Description |
|------|-------------|
| `/.well-known/openid-configuration` | OIDC discovery |
| `/.well-known/smart-configuration` | SMART metadata |
| `/oauth/authorize` | Authorization code + PKCE |
| `/oauth/token` | Token exchange (auth code, client credentials, refresh) |
| `/oauth/jwks` | Signing key set |
| `/oauth/register` | Dynamic client registration |

## Usage

```go
server, _ := oauth.NewServer(oauth.Config{
    Issuer:       "https://auth.example",
    FHIRAudience: "https://fhir.example",
})
server.RegisterClient(oauth.Client{
    ClientID:     "demo",
    RedirectURIs: []string{"https://app.example/callback"},
    Scopes:       []string{"patient/Patient.rs"},
})
http.Handle("/", server.Handler())

adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{DefaultTenantID: "t1"})
bearer := server.BearerAuthConfig(adapter)
```

Wire `hahttp.SMARTBearerPrincipalResolver(bearer)` on the FHIR handler to validate
access tokens issued by this server.

## Production notes

- Register explicit `RedirectURIs` for every client (empty lists are rejected).
- Set `ConsentHandler` or `RequireConsentForm`; use `AutoApprove` only in tests.
- Use `AuthorizationStore: oauth.NewFileAuthorizationStore(path)` for durable codes/tokens.
- Confidential clients must send `client_secret` on token exchange (basic auth or form field).
- Public clients must use PKCE (`code_challenge` / `code_verifier`).

See `examples/smart-oauth` for a combined OAuth + FHIR demo with consent and file-backed tokens.
