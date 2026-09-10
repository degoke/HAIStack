# OAuth operations guide

This document describes how to run the built-in `pkg/oauth` authorization server in **production-like** deployments versus the **Inferno reference** host used for SMART conformance testing.

## Deployment profiles

| Setting | Inferno reference (`infernotest`, `cmd/inferno-reference`) | Production-like host |
|---------|--------------------------------------------------------------|----------------------|
| Purpose | Inferno STU2 discovery + standalone launch CI | Edge, demo, air-gapped, integration |
| SQLite | Ephemeral temp DB per process | Persistent DB + migrations `0012`–`0018` |
| `ApplySQLiteStores` | Yes | Yes (required) |
| `ApplyProductionDefaults` | **No** — open DCR intentional | **Yes** — requires `RegistrationAccessToken` or disabled DCR |
| `RegistrationAccessToken` | Unset (open `POST /oauth/register`) | Set from env/secret manager |
| `AutoApprove` | `true` (silent authorize for tests) | `false` (interactive consent) |
| Consent sessions | SQLite + scheduled purge | SQLite + scheduled purge |
| Launch issuer secrets | Static demo values in code | Env/secret manager; rotate via `LaunchIssuers` |
| Redirect URIs | Inferno launcher + loopback http | https (loopback http for local dev only) |
| Client registry | Pre-seeded `inferno-reference` client | Static bootstrap + optional DCR |
| TLS | Often plain http on loopback | TLS termination required in production |

**Do not** point production traffic at `infernotest.BuildReferenceHandler` or `cmd/inferno-reference`. Those hosts trade security for conformance ergonomics.

## Inferno reference host

Used by:

- `go test ./pkg/testkit/infernotest/...`
- `go run ./cmd/inferno-reference` (manual Inferno test kit runs)
- `.github/workflows/inferno.yml`

Configuration is built in `pkg/testkit/infernotest/reference.go`:

- Issuer and FHIR base derived from listen address
- Pre-registered client `inferno-reference` with Inferno launcher redirect URI
- `AutoApprove = true` so authorize returns codes without consent UI
- Open dynamic client registration (no `RegistrationAccessToken`)
- Demo launch issuer credentials (`inferno-ehr` / `inferno-ehr-secret`)
- Ephemeral SQLite file removed on shutdown

SMART discovery: `{fhir_base}/.well-known/smart-configuration`

## Production-like host

Recommended bootstrap:

```go
cfg := oauth.Config{
    Issuer:      issuerURL,
    FHIRBaseURL: fhirBaseURL,
    Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "server-1"},
    Clients:     staticRegistry,
}
if err := store.ApplySQLiteStores(&cfg, db); err != nil {
    return err
}
cfg.RegistrationAccessToken = os.Getenv("OAUTH_REGISTRATION_TOKEN")
if err := oauth.ApplyProductionDefaults(&cfg); err != nil {
    return err
}
srv, err := oauth.NewServer(cfg)
if err != nil {
    return err
}
```

Environment variables (example):

| Variable | Purpose |
|----------|---------|
| `OAUTH_REGISTRATION_TOKEN` | Bearer token required for `POST /oauth/register` when DCR enabled |
| `HAISTACK_PRODUCTION=1` | Used by `examples/oauth-fhir-server` to call `ApplyProductionDefaults` |

Disable DCR entirely when not needed:

```go
disabled := false
cfg.DynamicClientRegistration = &disabled
```

## Consent session cleanup

Consent sessions expire after five minutes (`DefaultConsentSessionTTL`). SQLite stores purge expired rows opportunistically on create/consume; under high volume, run a background sweeper:

```go
consentCtx, stopConsent := context.WithCancel(ctx)
defer stopConsent()
oauth.StartConsentSessionCleanup(consentCtx, cfg.ConsentSessionStore, oauth.DefaultConsentSessionCleanupInterval)
```

`DefaultConsentSessionCleanupInterval` is five minutes. Use a shorter interval if authorize traffic is heavy and abandoned consent flows are common.

Manual purge (metrics, one-off maintenance):

```go
removed, err := cfg.ConsentSessionStore.PurgeExpired(ctx)
```

## Checklist before go-live

1. `ApplySQLiteStores` wired; migrations applied
2. `ApplyProductionDefaults` passes (registration token or DCR disabled)
3. `AutoApprove` false unless explicitly demo-only
4. Consent session cleanup goroutine running
5. Launch issuer credentials from secrets, not source code
6. TLS in front of OAuth and FHIR endpoints
7. Inferno reference host **not** exposed to untrusted networks

## See also

- [`README.md`](README.md) — API reference and endpoint list
- [`../testkit/README.md`](../testkit/README.md) — `infernotest` package usage
