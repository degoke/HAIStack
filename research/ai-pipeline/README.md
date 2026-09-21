# Track E — Reproducible FHIR → AI pipeline

End-to-end demonstration that HAIStack can turn **validated FHIR** into
**permissioned ViewDefinition rows**, invoke a **policy-governed AI tool**,
and emit an **auditable provenance chain**.

```
FHIR resources (synthetic, validated)
  → ViewDefinition projection (pkg/view)
  → permissioned row set (pkg/auth)
  → AI tool `run_view` (pkg/ai)
  → seeded stub model (no API keys)
  → audit log + provenance bundle (pkg/audit)
```

## Reproduce

```bash
make research-ai-pipeline
# or
go test ./research/ai-pipeline
go run ./research/ai-pipeline
```

The CLI prints a JSON provenance bundle to stdout. The bundle is
deterministic given the fixed clock (`2026-09-21T12:00:00Z`) and stub seed
`11`.

## FAIR metadata

| Field | Value |
|-------|-------|
| Identifier | `research/ai-pipeline` |
| License | Apache-2.0 |
| Data | Synthetic patients and observations; no PHI |
| IG / FHIR | FHIR R4 JSON; ViewDefinition `research_vitals_view` `1.0.0` |
| Policy | deny-by-default DSL in `pipeline.go` |
| Model | `stub-v1` deterministic adapter; no network |
| Audit | `pkg/audit` memory store; actions `execute-view` and `execute-tool` |

## What the bundle proves

- Each input resource has a canonical JSON hash.
- The view name, version, and row hashes are recorded.
- The policy document hash is recorded.
- Tool citations point at the view and source Observation ids.
- Audit events cover view execution and AI tool success.

This is an evaluation artefact, not a clinical model.
