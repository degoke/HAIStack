# SMART Authorization Architecture

HAIStack separates **authentication** (who is calling) from **authorization** (what they may do). A successful OAuth token exchange is not an authorization test.

## Layers

```
Bearer token / demo principal
        │
        ▼
PrincipalResolver + AuthBundleResolver (optional)
        │
        ▼
ScopePolicyAuthChecker ──► scope CRUDS gate (r/s/c/u/d)
        │
        ▼
auth.PolicyEngine ──► deny-by-default policy DSL
        │
        ▼
HTTP handler post-filters
   • patient compartment (TenantContext.PatientScope)
   • SMART 2.2 scope filters (?category=…)
```

## Decision order

1. **Authenticate** — resolve principal and tenant; optional SMART `AuthBundle` on context.
2. **Scope operation gate** — `ScopePolicyAuthChecker` verifies CRUDS letters for read/search/write.
3. **Policy** — `auth.Engine` evaluates roles, permissions, and policy rules.
4. **Patient compartment** — `CheckEnvelopePatientScope` on loaded resources and bundle entries.
5. **Scope filters** — intersect search params; post-filter bundles, history, and `$everything`.

Policy deny always overrides an apparently valid SMART scope.

## Host wiring

### Built-in authorization server (`pkg/oauth`)

```go
oauthServer, _ := oauth.NewServer(oauth.Config{Issuer: issuer, FHIRAudience: fhirBaseURL})
http.Handle("/", oauthServer.Handler()) // authorize, token, jwks, register, discovery

adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{...})
bearer := oauthServer.BearerAuthConfig(adapter)
```

### FHIR resource server

```go
handler, _ := hahttp.NewHandler(hahttp.Config{
    PrincipalResolver:  hahttp.SMARTBearerPrincipalResolver(bearer),
    AuthBundleResolver: hahttp.SMARTBearerBundleResolver(bearer),
    AuthChecker: smart.ScopePolicyAuthChecker{Engine: eng, Adapter: adapter, BundleFor: ...},
    ScopeFilterMatcher: smart.RegistryScopeFilterMatcherChain(searchRegistry, fhirpathEngine),
})
```

Serve SMART metadata from the OAuth server or separately:

```go
http.Handle("/.well-known/smart-configuration", smart.WellKnownHandler(smart.DefaultConfiguration(issuer)))
```

## Error contract

| Condition | HTTP | Issue code |
|-----------|------|------------|
| Missing/invalid credentials | 401 | `security` |
| Expired / not-yet-valid token | 401 | `security` |
| Replayed backend assertion | 401 | `security` |
| Policy or compartment deny | 403 | `forbidden` |
| Scope filter mismatch | 403 | `forbidden` |

Golden shapes live in `pkg/testkit/golden/auth_outcomes.go` and are asserted in HTTP
authz tests via `smart.StableAuthDiagnostics`. Scenario catalog: `pkg/testkit/authztest`.

## Scope filter matching

Registry-backed evaluators use compiled SearchParameter FHIRPath expressions (same
pipeline as `pkg/search` indexing):

Per-handler matchers are preferred over `smart.InstallRegistryScopeFilterMatcher` (global).

Unregistered parameters fall back to the built-in MVP matcher (`Observation.category`
coding walk; other params use top-level string fields).

Bundle `_include` / `_revinclude` entries accept scopes with either `r` or `s`.

## Conformance CI

`.github/workflows/smart-authz.yml` runs HTTP authz E2E tests, the authz scenario
catalog, `pkg/http` Inferno-style SMART-on-FHIR smoke (discovery + PKCE + FHIR read),
and `pkg/oauth` authorization-server tests.

## Built-in OAuth server (`pkg/oauth`)

Production deployment uses `oauthpostgres.NewServer` (`pkg/oauth/postgres`) with Postgres-backed client, token, replay, and revocation stores. Set `UserAuthenticator` for end-user consent, `LaunchResolver` for EHR launch, and persist `oauth-signing.pem` across restarts. File-backed `oauth.NewProductionServer` remains for single-node dev. See `pkg/oauth/README.md`.

## Non-goals

- Full third-party Inferno test-kit Docker runs (Go smoke tests cover core flows)

## References

- `pkg/smart/README.md` — scope formats and v1→v2 mapping
- `pkg/auth/README.md` — policy DSL
- `examples/smart-authz` — runnable restricted vs unrestricted principals
- `examples/smart-oauth` — built-in OAuth server + FHIR read with consent and file-backed tokens
