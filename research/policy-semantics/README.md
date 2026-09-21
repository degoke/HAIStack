# Track C — Computable consent and policy semantics

Formalizes HAIStack's deny-by-default policy DSL and its intersection with
SMART scopes and R4 Consent patterns. This is a vendor-neutral test catalogue,
not a production consent engine.

See [`SEMANTICS.md`](./SEMANTICS.md) for the decision algorithm and worked
examples. Machine-readable cases live in
[`testdata/scenarios.json`](./testdata/scenarios.json).

## Reproduce

```bash
make research-policy
go test ./research/policy-semantics -count=1
go run ./research/policy-semantics/cmd
```

The runner evaluates `principal + SMART scopes + consent state + request →
expected decision` against `pkg/auth` and `pkg/smart`. SMART scenarios add a
research-only `*.read` overlay on the clinician role so wildcard scopes can
satisfy `RequiredPermissions`; that is not production `pkg/auth` ∩ SMART
(see SEMANTICS.md). Declarative scenarios are also executable from
`pkg/testkit/authztest` (consent overlays are research-only and skipped there).

## Catalogue size

The published JSON includes **≥12** scope ∩ policy examples plus consent
permit/deny overlays. Each example in SEMANTICS.md has a matching scenario id.
