# pkg/oauth

Production-capable OAuth2/OIDC authorization server for SMART on FHIR.

## What it does

`pkg/oauth` implements a **built-in authorization server** you can mount next to FHIR HTTP handlers. It exposes standard OAuth2/OIDC and SMART discovery documents, runs authorization code + PKCE flows, mints JWT access tokens, and validates them for `pkg/smart` bearer middleware.

Capabilities include:

- OpenID Connect discovery and SMART configuration metadata
- Authorization code, refresh token, and client credentials grants
- Token revocation (refresh tokens and JWT access tokens by `jti`)
- RFC 7662 introspection for confidential clients
- JWKS publication and signing-key rotation hooks
- Optional dynamic client registration, HTML consent, and EHR launch orchestration
- Multi-tenant issuers under `/t/{tenantId}/oauth/*`

Durable state (clients, codes, refresh tokens, replay protection, revocation denylist, rate limits, DB signing keys) lives in `pkg/oauth/store` (Postgres or SQLite) or ephemeral Redis for token state with a durable client registry.

## How it fits in the ecosystem

```
 SMART app / backend service
        │
        ▼
 /.well-known/smart-configuration  ◄── pkg/oauth (this package)
 /oauth/authorize, /token, /jwks
        │
        ▼
 JWT access token ──► pkg/smart BearerAuth on pkg/http FHIR routes
        │
        ▼
 FHIR API (Patient.read, etc.)

 haistack serve: runtime.WithBuiltinOAuth wires stores + issuer from config
 pkg/smart:      scope validation, backend-service JWT client auth (separate file stores)
 pkg/auth:       identity/policy library used alongside tokens
```

Hosts that already use an external IdP (Auth0, Keycloak, Azure AD) typically **do not** enable built-in OAuth; they configure `pkg/smart` with the external issuer and JWKS instead.

## When to use it

| Scenario | Recommendation |
|----------|----------------|
| Local dev, demos, integration tests | `oauth.NewServer` with in-memory stores, or `examples/smart-oauth` |
| Single-node or small deployment | `oauthstore.NewSQLiteServer` |
| Production multi-instance cluster | `oauthstore.NewPostgresServer` (transactional consume, row locks) |
| Ephemeral token state at scale | `oauthredis.NewServer` + Postgres/SQLite `ClientRegistry` |
| Multi-tenant SaaS | `MultiTenantServer` with per-tenant issuer URLs |
| Embedded in custom binary | `oauth.NewServer` + `ApplyPostgresStores` / manual store wiring |
| Enterprise IdP already owns users | External OIDC; skip built-in OAuth |

## Usage modes

### Embedded in the haistack runtime

`haistack serve` enables OAuth when config sets `oauth.enabled`. Production mode requires issuer URL, `OAUTH_REGISTRATION_TOKEN`, `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET`, `OAUTH_SESSION_SECRET`, and `OAUTH_LOGIN_USERS` (see below).

```go
import "github.com/degoke/haistack/pkg/runtime"

rt, err := runtime.NewBuilder().
    WithBuiltinOAuth(runtime.BuiltinOAuthConfig{
        Production: true,
        IssuerURL:  "https://auth.example",
    }).
    Build(ctx)
// rt.Handler() serves FHIR + /oauth/* + well-known routes
```

`runtime.BuiltinOAuthConfig` resolves issuer defaults from HTTP listen address in non-production setups and scopes tenant issuers to `/t/{tenantId}/`.

### Standalone Postgres (or SQLite) server

Use store helpers for a dedicated auth service or custom `http.Server` mux:

```go
import (
    "github.com/degoke/haistack/pkg/oauth"
    oauthstore "github.com/degoke/haistack/pkg/oauth/store"
    "github.com/degoke/haistack/pkg/postgres"
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

Mount `server.Handler()` (or tenant handler from `MultiTenantServer`) on your router. Use `server.BearerAuthConfig(adapter)` so FHIR handlers validate JWTs minted by this issuer.

Lower-level wiring:

```go
var cfg oauth.Config
cfg.Issuer = "https://auth.example"
_ = oauthstore.ApplyPostgresStores(&cfg, db.Pool())
_ = oauthstore.ApplyPostgresSigningKey(&cfg, db.Pool(), cfg.Issuer, oauthstore.SigningKeyOptions{})
srv, err := oauth.NewServer(cfg)
```

### External IdP alternative

When customers bring their own authorization server:

1. Do not mount `pkg/oauth` (or disable `oauth.enabled`).
2. Configure `pkg/smart` with external issuer, JWKS URI, and audience for your FHIR base URL.
3. Keep `pkg/smart` backend-service client stores if machine clients use private_key_jwt against your FHIR tier — those file/DB stores are **not** the OAuth authorization-server stores documented here.

Built-in OAuth is for deployments that want SMART metadata, consent, and token minting **in-process** without operating a separate auth product.

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

## Core types

| Type | Role |
|------|------|
| `oauth.Config` | Issuer, audience, TTLs, consent, launch, rate limits, store interfaces |
| `oauth.Server` | HTTP handlers + token minting + `BearerAuthConfig` for SMART |
| `oauth.MultiTenantServer` | Routes requests by tenant id / issuer |
| `oauth.Client` / `ClientRegistry` | Registered SMART clients and auth methods |
| `oauth.LaunchResolver` | EHR launch context for SMART apps |
| `oauth.UserAuthenticator` | End-user identity for consent binding |

`NewServer` without durable stores uses in-memory authorization data — suitable for tests only. Production embedders should use `oauthstore.NewSQLiteServer` or `oauthstore.NewPostgresServer`.

## Production deployment (recommended: Postgres)

Use `oauthstore.NewPostgresServer` (or `ApplyPostgresStores` + `oauth.NewServer`) for
multi-instance clusters. Postgres provides transactional `DELETE … RETURNING` consume
semantics and row-level locking — no shared filesystem required.

```go
import (
    "github.com/degoke/haistack/pkg/oauth"
    oauthstore "github.com/degoke/haistack/pkg/oauth/store"
    "github.com/degoke/haistack/pkg/postgres"
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

Schema: migrations `0017_oauth.sql` + `0018_oauth_rate_limit.sql` + `0019_oauth_signing_key.sql` + `0020_oauth_issuer_binding.sql` + `0021_oauth_issuer_pk.sql` + `0022_oauth_client_issuer.sql` (Postgres), or `0014_oauth.sql` + `0015_oauth_rate_limit.sql` + `0016_oauth_signing_key.sql` + `0017_oauth_issuer_binding.sql` + `0018_oauth_issuer_pk.sql` + `0019_oauth_client_issuer.sql` (SQLite).

Auth codes, refresh tokens, and pending consent rows store an `issuer` column (and JSON `issuer` field) so a shared SQL store can enforce that tokens minted under `/t/{tenantId}/` are only consumed by the matching tenant issuer.

### Redis (ephemeral token state)

Use `oauthredis.NewServer` for TTL-backed auth codes, refresh tokens, replay JTIs, and
revocation denylist. **Client registration stays on Postgres or SQLite** — pass a durable
`cfg.Clients` registry; Redis does not store clients.

```go
import (
    "github.com/degoke/haistack/pkg/oauth"
    oauthstore "github.com/degoke/haistack/pkg/oauth/store"
    oauthredis "github.com/degoke/haistack/pkg/oauth/redis"
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
| `OAUTH_LOGIN_USERS` | Production login directory: `username:password` or `username:$2a$...` (newline or `;` separated) |

When `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET` is unset, signing keys fall back to PEM at `{state-dir}/oauth-signing.pem` (`oauth.DefaultSigningKeyPaths`). Set `OAUTH_SIGNING_KEY_ROTATE=1` before restart to rotate the active DB key.

Embedders that previously used `oauth.NewProductionServer` should call `oauthstore.NewSQLiteServer` or `oauthstore.NewPostgresServer` instead. Those APIs persist clients, authorization codes, refresh tokens, replay JTIs, and revocation across process restarts. `oauth.NewServer` without a store is in-memory only.

`pkg/smart` still has `FileBackendClientStore` / `FileReplayStore` for **SMART backend-service assertion** clients and `jti` replay — they are not the OAuth authorization-server stores.

## Multi-instance checklist

1. Use `oauthstore.NewPostgresServer` (recommended) or `oauthstore.NewSQLiteServer` for single-node.
2. Persist signing keys in DB (`OAUTH_SIGNING_KEY_ENCRYPTION_SECRET`) or `oauth-signing.pem` across restarts.
3. Set `UserAuthenticator` with a real user directory (or `haistack serve` production session login via `OAUTH_LOGIN_USERS`) for end-user consent binding.
4. Keep `AutoApprove: false` in production.
5. Enable `AllowDynamicRegistration` only when required.
6. Mount tenant routes at `/t/{tenantId}/` when using `MultiTenantServer`.

Use `MultiTenantBearerAuth` (`wire.go`) when FHIR and OAuth share a mux but JWT validation must use tenant-specific issuers.

See `examples/smart-oauth` for a runnable demo, or `haistack serve` for built-in OAuth with SQLite/Postgres stores (`runtime.WithBuiltinOAuth`). Operations guidance: `OPERATIONS.md`. See [doc.go](./doc.go) for `pkg/http` and `pkg/runtime` integration.
