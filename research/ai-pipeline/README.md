# Track E — Reproducible FHIR → AI pipeline

End-to-end, deterministic demonstration of HAIStack's safe-AI path:

```
synthetic FHIR (structurally validated)
  → ViewDefinition projection (pkg/view)
  → permissioned row set (pkg/auth)
  → AI tool invocation (pkg/ai run_view)
  → seeded model stub (no API keys)
  → audit log + exportable provenance bundle (pkg/audit)
```

## Reproduce

```bash
make research-ai-pipeline
# or
go test ./research/ai-pipeline -count=1
go run ./research/ai-pipeline/cmd
```

The command prints a provenance bundle on stdout. Hashes, view version, policy
version, tool name, stub seed, **sequential audit event IDs**, and the full
bundle JSON are byte-stable across runs (fixed clock `2026-09-21T13:00:00Z`,
stub seed `11`, `NewID` `audit-01`…). If the denied actor is not actually
denied, `Run` returns an error and the command exits without printing a
bundle.

## Dataset

Three synthetic patients and six observations (some `final`, some
`preliminary`). The lab view keeps only final rows. There is no PHI.

Conformance pinning to a published IG is tracked separately; this pipeline
validates resources with `pkg/validate` against FHIR R4 structural rules.

## Provenance chain

The bundle records:

| Field | Source |
|-------|--------|
| `inputs[].hash` | `ResourceEnvelope.Hash` (canonical JSON) |
| `view.name` / `view.version` | registered ViewDefinition |
| `policy.hash` | SHA-256 of the policy document |
| `tool.name` | `run_view` |
| `model.adapter` / `model.seed` | seeded stub |
| `output` | stub summary of permissioned context |
| `citations` | view + resource citations from `pkg/ai` |
| `audit[]` | `execute-view` and `execute-tool` events |

Denied paths (wrong actor / wrong view) are required: `Run` fails if the
denied actor is allowed, so `go run` cannot publish a success-only bundle.
