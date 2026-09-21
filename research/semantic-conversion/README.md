# Track B — R4/R5 semantic conversion corpus

Paired synthetic FHIR instances for scoring structural equivalence, semantic
equivalence (FHIRPath assertions), and information-loss flags on R4→R5
migration. **R6 is out of scope** for this issue.

## Reproduce

```bash
make research-conversion
go test ./research/semantic-conversion -count=1
go run ./research/semantic-conversion/cmd
```

The scorer always emits at least 50 pair results. Categories:

| Category | What it exercises |
|----------|-------------------|
| `unchanged` | Elements with the same path in R4 and R5 |
| `renamed` | R4 `Condition.asserter` → R5 `Condition.participant` |
| `cardinality` | Singleton vs list (for example `interpretation`) |
| `type-change` | `MedicationRequest.reported[x]` boolean vs reference |
| `codeableconcept` | `reasonCode`/`reasonReference` → R5 `reason` CodeableReference |

## Scoring

For each pair:

1. **Structural** — `ConvertR4ToR5(r4)` canonical JSON matches expected R5
   for compared paths (or identity for `unchanged`).
2. **Semantic** — FHIRPath assertions hold on both versions (possibly with
   version-specific expressions).
3. **Information loss** — flags declared on the pair (dropped `Patient.animal`,
   collapsed reported reference, …) must appear in the score.

HAIStack's production codec is R4 (`pkg/proto.GoogleR4Codec`). R5 encoding is
planned (`google_r5.go`). This corpus does not require a live R5 protobuf
codec; envelopes use canonical JSON.

## HL7 version conversion packages

HL7 publishes FHIR version conversion maps (for example
`hl7.fhir.uv.xver` / the core conversion maps). This corpus is **not** a
reimplementation of those maps. It is a small, citable test set focused on
four resource types (Patient, Observation, Condition, MedicationRequest) with
explicit information-loss flags so pipelines can be scored even when a
converter is lossy by design.
