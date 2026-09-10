# pkg/oauth

Production-capable OAuth2/OIDC authorization server for SMART on FHIR.

## Endpoints

| Path | Description |
|------|-------------|
| `/.well-known/openid-configuration` | OIDC discovery |
| `/.well-known/smart-configuration` | SMART metadata |
| `/oauth/authorize` | Authorization code + PKCE |
| `/oauth/token` | Token exchange (auth code, client credentials, refresh) |
| `/oauth/revoke` | Refresh-token revocation (RFC 7009) |
| `/oauth/jwks` | Signing key set |
| `/oauth/register` | Dynamic client registration (opt-in) |
| `/oauth/consent` | Built-in HTML consent form |
| `/oauth/launch` | EHR launch context (JSON) |
| `/oauth/launch/ui` | EHR launch orchestration page |

## Quick start (development)

```go
server, _ := oauth.NewServer(oauth.Config{
    Issuer:       "https://auth.example",
    FHIRAudience: "https://fhir.example",
})
_ = server.RegisterClient(oauth.Client{
    ClientID:     "demo",
    RedirectURIs: []string{"https://app.example/callback"},
    Scopes:       []string{"patient/Patient.rs"},
})
http.Handle("/", server.Handler())
```

## Production deployment

Use durable file-backed stores and a persistent signing key:

```go
paths := oauth.DefaultProductionPaths("/var/lib/haistack/oauth")
server, err := oauth.NewProductionServer(oauth.Config{
    Issuer:             "https://auth.example",
    FHIRAudience:       "https://fhir.example",
    RequireConsentForm: true,
    LaunchResolver:     myLaunchResolver,
    UserAuthenticator:  myUserAuthenticator,
}, paths)
```

`NewProductionServer` wires:

- `FileAuthorizationStore` — auth codes, refresh tokens, consent sessions (shared across AS replicas)
- `FileClientStore` — registered clients with bcrypt-hashed secrets
- `FileReplayStore` — backend assertion `jti` replay protection
- `LoadKeySetFromPEM` — stable JWT signing when `oauth-signing.pem` exists

### Multi-instance checklist

1. Mount a shared state directory (NFS/EBS) or migrate stores to Postgres/Redis implementing the same interfaces.
2. Persist `oauth-signing.pem` and set `SigningKey` explicitly on first boot.
3. Set `UserAuthenticator` so consent binds to a real end-user `sub` / `fhirUser`.
4. Keep `AutoApprove: false` in production; use `RequireConsentForm` or a custom `ConsentHandler`.
5. Enable dynamic registration only when needed: `AllowDynamicRegistration: true`.
6. Register explicit `RedirectURIs` for every client.

### Client authentication

| Method | Server | Client SDK |
|--------|--------|------------|
| `client_secret_post` | Supported | `ClientAuth: client.ClientAuthSecretPost` (default) |
| `client_secret_basic` | Supported | `ClientAuth: client.ClientAuthSecretBasic` |
| `private_key_jwt` | `client_credentials` grant | `ExchangeClientAssertion` |

Set `TokenEndpointAuthMethod` on each `Client` registration; the server enforces the configured method.

### SMART launch

```go
server, _ := oauth.NewServer(oauth.Config{
    LaunchResolver: oauth.StaticLaunchResolver(oauth.LaunchContext{
        PatientID: "pat-1", Encounter: "enc-1",
    }),
})
```

- `/oauth/launch?launch=...&iss=...` returns JSON launch context
- `/oauth/launch/ui` validates `client_id` + `redirect_uri`, shows resolved patient/encounter, and forwards PKCE params to authorize

Wire `hahttp.SMARTBearerPrincipalResolver(bearer)` on the FHIR handler to validate access tokens issued by this server.

See `examples/smart-oauth` for a combined OAuth + FHIR demo with production stores, consent, and launch.
