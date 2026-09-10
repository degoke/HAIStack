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

```go
adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{...})
bearer := smart.BearerAuthConfig{Validator: tv, Adapter: adapter, Options: opts}

handler, _ := hahttp.NewHandler(hahttp.Config{
    PrincipalResolver:  hahttp.SMARTBearerPrincipalResolver(bearer),
    AuthBundleResolver: hahttp.SMARTBearerBundleResolver(bearer),
    AuthChecker: smart.ScopePolicyAuthChecker{Engine: eng, Adapter: adapter, BundleFor: ...},
})
```

Serve SMART metadata separately:

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

Golden shapes live in `pkg/testkit/golden/auth_outcomes.go`. Scenario catalog: `pkg/testkit/authztest`.

## Scope filter matching (MVP)

SMART 2.2 `?param=value` filters are enforced, but resource matching is intentionally
narrow today:

- `Observation.category` — FHIR coding comparison
- Other parameters — top-level string field fallback

Bundle `_include` / `_revinclude` entries accept scopes with either `r` or `s`.
Hosts with complex filter matrices should extend `pkg/smart` with registry-backed
parameter evaluators.

## Non-goals

- OAuth2/OIDC authorization server
- EHR launch UI orchestration
- Full Inferno certification (see `.github/workflows/smart-authz.yml` smoke job)

## References

- `pkg/smart/README.md` — scope formats and v1→v2 mapping
- `pkg/auth/README.md` — policy DSL
- `examples/smart-authz` — runnable restricted vs unrestricted principals
