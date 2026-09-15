# OAuth operations guide

This document describes how to run the built-in `pkg/oauth` authorization server in **production-like** deployments versus the **Inferno reference** host used for SMART conformance testing.

## Deployment profiles

| Setting | Inferno reference (`infernotest`, `cmd/inferno-reference`) | `haistack serve` production |
|---------|----------------------------------------------------------|-----------------------------|
| Purpose | Inferno STU2 discovery + standalone launch CI | Edge, demo, air-gapped |
| SQLite | Ephemeral temp DB per process | Persistent DB + migration `0014_oauth.sql` |
| Postgres | N/A | Persistent DB + migration `0017_oauth.sql` |
| `store.ApplySQLiteStores` / `ApplyPostgresStores` | Yes | Yes (via `runtime.WithBuiltinOAuth`) |
| `RegistrationAccessToken` | Unset (open DCR) | Required when `oauth.production` is true |
| `AutoApprove` | `true` | `false` in production |
| Signing key | Ephemeral per process | DB keys with `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET`, or PEM fallback |
| User login | N/A | `/oauth/login` session cookies via `OAUTH_SESSION_SECRET` |
| Rate limits | In-memory | DB-backed (`hai_oauth_rate_limit`) |
| Introspection | Available | `POST /oauth/introspect` (confidential clients) |
| Tenant routes | N/A | `/t/{tenantId}/oauth/*` |
| TLS | Plain http on loopback | Pin `oauth.issuerURL` to https in production |

**Do not** point production traffic at `infernotest.BuildReferenceHandler` or `cmd/inferno-reference`.

## Inferno reference host

- `go test ./pkg/testkit/infernotest/...`
- `go run ./cmd/inferno-reference`
- `.github/workflows/inferno.yml`

Built in `pkg/testkit/infernotest/reference.go` with open DCR, `AutoApprove`, and ephemeral SQLite.

## haistack serve

Enable with `oauth.enabled: true` (default). Production checklist:

1. Set `oauth.issuerURL` to your public https issuer.
2. Set `OAUTH_REGISTRATION_TOKEN` (or `oauth.registrationAccessToken`).
3. Set `OAUTH_SIGNING_KEY_ENCRYPTION_SECRET` for DB-backed signing keys (recommended).
4. Set `OAUTH_SESSION_SECRET` for production consent login sessions.
5. Set `oauth.production: true` and `oauth.autoApprove: false`.
6. Back up signing keys (DB table `hai_oauth_signing_key` or PEM at `{sqlite-dir}/oauth/oauth-signing.pem`).

SMART discovery is served at `/.well-known/smart-configuration` and mirrored under `/fhir/.well-known/smart-configuration`.
