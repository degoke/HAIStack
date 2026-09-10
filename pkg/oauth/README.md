# pkg/oauth

Production-capable OAuth2/OIDC authorization server for SMART on FHIR.

## Endpoints

| Path | Description |
|------|-------------|
| `/.well-known/openid-configuration` | OIDC discovery |
| `/.well-known/smart-configuration` | SMART metadata |
| `/oauth/authorize` | Authorization code + PKCE |
| `/oauth/token` | Token exchange (auth code, client credentials, refresh) |
| `/oauth/revoke` | Revoke refresh tokens and JWT access tokens (by `jti`) |
| `/oauth/jwks` | Signing key set |
| `/oauth/register` | Dynamic client registration (opt-in) |
| `/oauth/consent` | Built-in HTML consent form |
| `/oauth/launch` | EHR launch context (JSON) |
| `/oauth/launch/ui` | EHR launch orchestration page |

## Production deployment (recommended: Postgres)

Use `oauthpostgres.NewServer` for multi-instance clusters. Postgres provides transactional
`DELETE … RETURNING` consume semantics and row-level locking — no shared filesystem required.

```go
import (
    "github.com/degoke/health-ai-stack/pkg/oauth"
    oauthpostgres "github.com/degoke/health-ai-stack/pkg/oauth/postgres"
    "github.com/degoke/health-ai-stack/pkg/postgres"
)

db, _ := postgres.Open(ctx, dsn)
_ = db.Migrate(ctx)

server, err := oauthpostgres.NewServer(oauth.Config{
    Issuer:             "https://auth.example",
    FHIRAudience:       "https://fhir.example",
    RequireConsentForm: true,
    LaunchResolver:     myLaunchResolver,
    UserAuthenticator:  myUserAuthenticator,
}, db.Pool())
```

`oauthpostgres.Stores` wires:

- `AuthorizationStore` — auth codes, refresh tokens, consent sessions
- `ClientRegistry` — clients with bcrypt-hashed secrets
- `ReplayStore` — backend/client-assertion `jti` replay protection
- `RevocationStore` — revoked access-token `jti` denylist

Schema: migration `0015_oauth.sql`.

### Why file stores existed

Early iterations used `FileAuthorizationStore` for a **zero-dependency** way to share
OAuth state across a few AS replicas on a mounted volume. That works for dev/small
deployments but is a poor fit for production:

- No cross-host locking (NFS latency and corruption risk)
- Full-file rewrite on every token operation
- No HA failover semantics

`NewProductionServer` (file-backed) remains for single-node and test environments.
**Postgres is the recommended production path.**

### Redis (high-throughput cache)

Use `oauthredis.NewServer` when you want a shared in-memory cache instead of relational storage:

```go
import (
    "github.com/degoke/health-ai-stack/pkg/oauth"
    oauthredis "github.com/degoke/health-ai-stack/pkg/oauth/redis"
    goredis "github.com/redis/go-redis/v9"
)

rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
server, err := oauthredis.NewServer(oauth.Config{...}, rdb, "hai:oauth:")
```

`oauthredis.Stores` implements the same four interfaces with TTL-based keys.

### File-backed alternative (single node / dev)

```go
paths := oauth.DefaultProductionPaths("/var/lib/haistack/oauth")
server, err := oauth.NewProductionServer(oauth.Config{...}, paths)
```

## Client authentication at the token endpoint

| Method | Grants | Server | Client SDK |
|--------|--------|--------|------------|
| `client_secret_post` | auth code, refresh, revoke | Supported | `ClientAuthSecretPost` (default) |
| `client_secret_basic` | auth code, refresh, revoke | Supported | `ClientAuthSecretBasic` |
| `private_key_jwt` | auth code, refresh, revoke, client credentials | Supported | `ClientAuthPrivateKeyJWT` + `ClientJWT` |

Set `TokenEndpointAuthMethod` on each registered `Client`.

### private_key_jwt example (auth code exchange)

```go
form.Set("grant_type", "authorization_code")
form.Set("code", code)
form.Set("redirect_uri", redirectURI)
form.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:jwt-bearer")
form.Set("client_assertion", signedJWT)
form.Set("code_verifier", pkceVerifier)
```

## Access-token revocation

`POST /oauth/revoke` accepts:

- `refresh_token` — deletes the refresh token when it belongs to the authenticated client
- `access_token` (or `token_type_hint=access_token`) — adds the JWT `jti` to the revocation denylist when the token's `client_id` claim matches

Revoked access tokens are rejected by `server.BearerAuthConfig()` via `TokenValidateOptions.IsJWTRevoked`.
Access tokens include a `client_id` claim for ownership checks.

## Multi-instance checklist

1. Use `oauthpostgres.NewServer` (recommended) or shared file stores for dev only.
2. Persist `oauth-signing.pem` across restarts (`LoadKeySetFromPEM`).
3. Set `UserAuthenticator` for end-user consent binding.
4. Keep `AutoApprove: false` in production.
5. Enable `AllowDynamicRegistration` only when required.

See `examples/smart-oauth` for a runnable demo.
