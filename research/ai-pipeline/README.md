# Track E — Reproducible FHIR → AI pipeline

End-to-end demonstration that HAIStack can turn **validated FHIR** into
**authorized ViewDefinition output**, invoke a **policy-governed AI tool**,
and emit an **auditable provenance chain**.

```
FHIR resources (synthetic, validated)
  → ViewDefinition projection (pkg/view)
  → authorized view/tool execution (pkg/auth; full authorized view output,
    not per-row ACL filtering or a deny-path demo)
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
| IG / FHIR | FHIR R4 `4.0.1` base Patient/Observation SDs **and** compiled HAIStack IG `hai-patient` from `modules/core/ig`; package/version from `conformance-lock.json` (`haistack.fhir.r4`). `conformanceLockCommit` is the lock rewrite; `checkoutCommit` is this tree. |
| Policy | deny-by-default DSL in `pipeline.go` |
| Model | `stub-v1` deterministic adapter; no network |
| Store | `research/internal/researchutil.MemoryResourceStore` (not `pkg/testkit`) |
| Audit | `pkg/audit` memory store; actions `execute-view`, `execute-tool`, and `invoke-model` |

## What the bundle proves

- Each input resource has a canonical JSON hash and was validated against the recorded profile (`hai-patient` for Patient, HL7 R4 base for Observation).
- The view name, version, and row hashes are recorded for the **full
  authorized view output** (no per-row ACL filtering in this artefact).
- The policy document hash is recorded.
- The tool **name** is recorded (not a separate tool-version field).
- Tool citations point at the view and source Observation ids.
- Audit events cover view execution, AI tool success, and stub model invocation (`pkg/ai.Executor.InvokeModel`).

This is an evaluation artefact, not a clinical model.
