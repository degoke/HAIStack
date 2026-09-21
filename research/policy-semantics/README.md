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
| [scenarios.yaml](scenarios.yaml) | Machine-readable cases: principal + scopes + policy + request → expected decision |
| [consent-patterns.md](consent-patterns.md) | R4 Consent patterns and a sketch of R5/R6 Permission |

## Runner

`pkg/testkit/authztest.ParseYAML` / `ScenariosFromYAML` is the shared
scenario runner (also used by the authorization test suite). YAML cases
fail unless **both** SMART `ScopeImplies` and the policy engine allow the
action.

This catalogue is vendor-neutral: the YAML does not mention HAPI, Firely,
or other servers. Adapters can replay the same principal/scope/request
tuples against another authorization engine.
