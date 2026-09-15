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
| `/oauth/introspect` | RFC 7662 token introspection (confidential clients only) |
| `/oauth/jwks` | Signing key set |
| `/oauth/login` | Session login for production consent (when `UserAuthenticator` is configured) |
| `/t/{tenantId}/oauth/*` | Tenant-scoped OAuth routes (via `MultiTenantServer`) |
| `/oauth/register` | Dynamic client registration (opt-in) |
| `/oauth/consent` | Built-in HTML consent form |
| `/oauth/launch` | EHR launch context (JSON) |
| `/oauth/launch/ui` | EHR launch orchestration page |

## Production deployment (recommended: Postgres)

Use `oauthstore.NewPostgresServer` (or `ApplyPostgresStores` + `oauth.NewServer`) for
multi-instance clusters. Postgres provides transactional `DELETE … RETURNING` consume
semantics and row-level locking — no shared filesystem required.

```go
import (
    "github.com/degoke/health-ai-stack/pkg/oauth"
    oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
    "github.com/degoke/health-ai-stack/pkg/postgres"
)

db, _ := postgres.Open(ctx, dsn)
_ = db.Migrate(ctx)

server, err := oauthstore.NewPostgresServer(oauth.Config{
    Issuer:             "https://auth.example",
    FHIRAudience:       "https://fhir.example",
    RequireConsentForm: true,
    LaunchResolver:     myLaunchResolver,
    UserAuthenticator:  myUserAuthenticator,
}, db.Pool())
```

`oauthstore.PostgresStores` wires:

- `AuthorizationStore` — auth codes, refresh tokens, consent sessions
- `ClientRegistry` — clients with bcrypt-hashed secrets
- `ReplayStore` — backend/client-assertion `jti` replay protection
- `RevocationStore` — revoked access-token `jti` denylist
- `TokenRateLimiter` / `RegisterRateLimiter` — DB-backed endpoint rate limits
- DB signing keys via `ApplyPostgresSigningKey` / `ApplySQLiteSigningKey` when `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET` is set

Schema: migrations `0017_oauth.sql` + `0018_oauth_rate_limit.sql` + `0019_oauth_signing_key.sql` (Postgres), or `0014_oauth.sql` + `0015_oauth_rate_limit.sql` + `0016_oauth_signing_key.sql` (SQLite).

### Why file stores existed

Early iterations used `FileAuthorizationStore` for a **zero-dependency** way to share
OAuth state across a few AS replicas on a mounted volume. That works for dev/small
deployments but is a poor fit for production:

- No cross-host locking (NFS latency and corruption risk)
- Full-file rewrite on every token operation
- No HA failover semantics

`NewProductionServer` (file-backed) remains for single-node and test environments.
**Postgres is the recommended production path.**

### Redis (ephemeral token state)

Use `oauthredis.NewServer` for TTL-backed auth codes, refresh tokens, replay JTIs, and
revocation denylist. **Client registration stays on Postgres or file** — pass a durable
`cfg.Clients` registry; Redis does not store clients.

```go
import (
    "github.com/degoke/health-ai-stack/pkg/oauth"
    oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
    oauthredis "github.com/degoke/health-ai-stack/pkg/oauth/redis"
    goredis "github.com/redis/go-redis/v9"
)

db, _ := postgres.Open(ctx, dsn)
_, clientStore, _, _ := oauthstore.PostgresStores(db.Pool())
rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
server, err := oauthredis.NewServer(oauth.Config{
    Issuer:  "https://auth.example",
    Clients: clientStore,
}, rdb, "hai:oauth:")
```

`oauthredis.EphemeralStores` wires the three ephemeral interfaces with TTL-based keys.

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
- `access_token` (or `token_type_hint=access_token`) — adds the JWT `jti` to the revocation denylist when the token's `client_id` claim matches the authenticated client (required)

Revoked access tokens are rejected by `server.BearerAuthConfig()` via `TokenValidateOptions.IsJWTRevoked`.
All access tokens include a `client_id` claim; revoke rejects tokens without it.

## Production environment variables

| Variable | Purpose |
|----------|---------|
| `OAUTH_REGISTRATION_TOKEN` | Bearer token for `POST /oauth/register` |
| `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET` | AES key for DB-stored signing keys (required for `haistack serve` production) |
| `OAUTH_SESSION_SECRET` | HMAC secret for `/oauth/login` session cookies (required for production consent) |

When `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET` is unset, signing keys fall back to PEM at `{state-dir}/oauth-signing.pem`.

## Multi-instance checklist

1. Use `oauthstore.NewPostgresServer` (recommended) or shared file stores for dev only.
2. Persist signing keys in DB (`OAUTH_SIGNING_KEY_ENCRYPTION_SECRET`) or `oauth-signing.pem` across restarts.
3. Set `UserAuthenticator` (or use `haistack serve` production session login) for end-user consent binding.
4. Keep `AutoApprove: false` in production.
5. Enable `AllowDynamicRegistration` only when required.
6. Mount tenant routes at `/t/{tenantId}/` when using `MultiTenantServer`.

See `examples/smart-oauth` for a runnable demo, or `haistack serve` for built-in OAuth with SQLite/Postgres stores (`runtime.WithBuiltinOAuth`). Operations guidance: `OPERATIONS.md`.
