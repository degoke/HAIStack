# Track B — Semantic conversion corpus (R4 → R5)

Paired FHIR instances documenting R4 → R5 mapping classes, with a scorer
that reports **structural equivalence**, **semantic equivalence**
(FHIRPath assertions), and **information-loss flags**.

HAIStack `pkg/proto` currently ships Google FHIR **R4** only. R5 JSON in
this corpus is the expected target shape; it is not round-tripped through
an R5 codec (out of scope for issue #11). R4 inputs are parsed with
`pkg/proto.NewGoogleR4Codec()`.

## Reproduce

```bash
make research-conversion
# or
go test ./research/semantic-conversion
go run ./research/semantic-conversion
```

The generator produces **≥50 pairs** across Patient, Observation,
Condition, and MedicationRequest. Categories:

| Category | What it exercises |
|----------|-------------------|
| `identity` | Stable elements (id, status, subject, quantities) |
| `cardinality` | e.g. Observation.specimen 0..1 → 0..* |
| `type` | e.g. Observation.bodySite CodeableConcept → BackboneElement; MedicationRequest.medication |
| `renamed` | e.g. Condition.asserter → participant; MedicationRequest.reasonCode → reason |
| `codeableconcept` | Coding system/display shifts that remain conceptually same |
| `information_loss` | R4 elements with no R5 counterpart (Condition.evidence, MedicationRequest.detectedIssue) |

## Scoring

For each pair:

1. Parse R4 JSON with `pkg/proto` (must succeed).
2. Structural: compare documented stable paths on R4 vs R5 JSON.
3. Semantic: FHIRPath assertions. R4 expressions that the engine can
   evaluate against the proto envelope are run through `pkg/fhirpath`.
   R5 (and `ofType()` choice) assertions use the same FHIRPath surface
   evaluated against canonical JSON, because `pkg/proto` has no R5 codec yet.
4. Record `informationLoss` flags from the pair metadata.

Relationship to HL7: pairs follow published R4/R5 resource diffs where a
mapping is defined. They are not a substitute for the HL7 version
conversion maps (`hl7.fhir.uv.xver` and related packages).

R6 is reserved; this corpus starts at R4→R5.
