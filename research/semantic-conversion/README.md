# Track B — Semantic conversion corpus (R4 → R5)

Paired FHIR instances documenting R4 → R5 mapping classes, with a scorer
that reports **structural equivalence**, **semantic equivalence**, and
**information-loss flags**.

This artefact is a **catalogue scorer**, not a converter. `ScoreAll`
checks embedded R4/R5 JSON pairs. It does **not** transform R4 into R5.
HAIStack `pkg/proto` currently ships Google FHIR **R4** only; a live R5
codec is out of scope for issue #11. R4 inputs are parsed with
`pkg/proto.NewGoogleR4Codec()`. The CLI report sets `"mode": "catalogue"`
and `"converter": false`.

## Reproduce

```bash
make research-conversion
# or
go test ./research/semantic-conversion
go run ./research/semantic-conversion
```

The generator produces **≥50 pairs** across Patient, Observation,
Condition, and MedicationRequest. **≥30 pairs** have distinct R4 and R5
JSON. Categories `renamed`, `type`, `cardinality`, and `information_loss`
always differ between versions.

`identity` documents **stable paths** (id, status, subject, quantities).
R4 and R5 JSON may still differ on mapped fields that every R5 instance
must carry — notably MedicationRequest `medicationCodeableConcept` →
`medication.concept`. Every MedicationRequest R5 body uses R5
`medication` (CodeableReference), not R4 `medication[x]`.

| Category | What it exercises |
|----------|-------------------|
| `identity` | Unique stable instances; StablePaths match across versions |
| `cardinality` | Shape/count changes, e.g. Observation.specimen 0..1 → 0..* |
| `type` | Choice or type shifts, e.g. Observation.bodySite CodeableConcept → BackboneElement; MedicationRequest.medication[x] → CodeableReference |
| `renamed` | Field moves, e.g. Condition.asserter → participant; MedicationRequest.reasonCode → reason |
| `codeableconcept` | Same concept with display/text presentation changes |
| `information_loss` | R4 elements with no R5 counterpart (Condition.evidence, MedicationRequest.detectedIssue) or converter drops (Patient.photo) |

## Scoring

For each pair:

1. Parse R4 JSON with `pkg/proto` (must succeed).
2. Structural: compare documented stable paths on R4 vs R5 JSON.
3. Semantic (R4): `pkg/fhirpath.EvalBool` against the Google R4 proto
   envelope. There is no JSON-walker fallback; a failing expression fails
   the pair.
4. Semantic (R5): structural JSON checks (`exists`, `missing`, `count`,
   `equals`) on the expected R5 document. These are **not** FHIRPath —
   `pkg/fhirpath` has no R5 codec.
5. `informationLoss` flags must be **present on R4** and **absent on R5**
   (including `extension[url]`). A flag for a field that never existed on R4
   fails the pair.

Relationship to HL7: pairs follow published R4/R5 resource diffs where a
mapping is defined. They are not a substitute for the HL7 version
conversion maps (`hl7.fhir.uv.xver` and related packages).

R6 is reserved; this corpus starts at R4→R5.
