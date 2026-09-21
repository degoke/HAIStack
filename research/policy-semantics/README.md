# Track C — Computable consent and policy semantics

Formalizes HAIStack authorization as **SMART scope grants ∩ pkg/auth policy
allows**, with a vendor-neutral scenario catalogue and a shared runner in
`pkg/testkit/authztest`.

## Reproduce

```bash
make research-policy
# or
go test ./research/policy-semantics
go run ./research/policy-semantics
```

The CLI loads `scenarios.yaml` (≥10 cases) and executes them against
`pkg/auth` + `pkg/smart`.

## Documents

| File | Contents |
|------|----------|
| [SEMANTICS.md](SEMANTICS.md) | Decision algorithm, first-match, deny-by-default, patient overlay, scope ∩ policy |
| [scenarios.yaml](scenarios.yaml) | Machine-readable cases: principal + scopes + policy + request → expected decision (no consent-state field) |
| [consent-patterns.md](consent-patterns.md) | R4 Consent / R5 Permission compile sketches; not an in-engine Consent suite |

## Runner

`pkg/testkit/authztest.ParseYAML` / `ScenariosFromYAML` is the shared
scenario runner requested by issue #11 (also used by the authorization
test suite). YAML cases fail unless **both** SMART `ScopeImplies` and the
policy engine allow the action. Policy documents, the role catalog, and
principals are declared in `scenarios.yaml` (portable `pkg/auth` DSL), not as
Go-named enums or hardcoded fixture IDs. YAML principal `id` / `kind` /
`tenant` / `roles` drive the SMART adapter. Tenant and kind are required on
every principal. The CLI runs `ScenariosFromYAML` then `sc.Run(ctx, nil)`.
Tests call `authztest.RunYAML`. Neither uses a Go `BaseConfig` kit.
`policyRoleGrants` overlays extra role permissions per named policy
(clinician `*.read` only for `observation-only`). Per-scenario `roleGrants`
do the same for deny-by-default, first-match, view, and AI-tool examples so
those cases pass SMART `RequiredPermissions` after `user/*.read` and then
fail or allow at the policy gate. Every scenario must declare `principal`,
`scopes`, `action`, `resourceType`, and `policy` (or `policyDocument`). Track C is the exception to
“testkit is tests-only”: the YAML catalogue is the artefact. Tracks A and E
do **not** import `pkg/testkit`.

This catalogue is vendor-neutral: the YAML does not mention HAPI, Firely,
or other servers. Adapters can replay the same principal/scope/request
tuples against another authorization engine.
