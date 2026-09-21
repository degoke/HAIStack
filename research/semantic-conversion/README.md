# Track B — R4/R5 semantic conversion corpus

Paired synthetic FHIR instances for scoring structural equivalence, semantic
equivalence, and information-loss flags on R4→R5 migration. **R6 is out of
scope** for this issue.

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
| `removed` | R4 `Patient.animal` is absent in R5 |
| `renamed` | R4 `Condition.asserter` → R5 `Condition.participant` |
| `cardinality` | Singleton vs list (for example `interpretation`) |
| `type-change` | `MedicationRequest.reported[x]` boolean vs reference |
| `codeableconcept` | `reasonCode`/`reasonReference` → R5 `reason` CodeableReference |

## Scoring

Gold pairs live in [`testdata/corpus.json`](./testdata/corpus.json). That file
is an **in-repo authored R5 oracle**, not a third-party mapping
(`hl7.fhir.uv.xver` / core conversion maps). Unchanged pairs are identical
in R4 and R5 in testdata because those elements did not change; that
identity is an authorship invariant checked without running the converter.
Transformed pairs cite the R5 resource diff (`spec`) and include mapping
fields that are not on the R4 instance:

| Category | Authored R5 constraint (absent from R4) |
|----------|-----------------------------------------|
| `removed` | R5 omits `animal`; pair cites the Patient R5 diff |
| `renamed` | `Condition.participant.function` informant system **and** display |
| `cardinality` | `Observation.interpretation` list plus v3 interpretation system/display |
| `codeableconcept` / `type-change` | `MedicationRequest.medication` CodeableReference; toy-med system on coded medication |

`ConvertR4ToR5` is an implementation scored against that oracle. Structural
equality (`Convert(r4) == gold R5`) proves the converter follows gold; it
does not prove gold was produced by an external mapper. Authorship tests
inspect testdata only and do not call `ConvertR4ToR5`.

For each pair:

1. **Structural** — canonical JSON of `ConvertR4ToR5(r4)` equals the gold R5
   document for that pair id.
2. **Semantic (R4)** — assertions run with `pkg/fhirpath` against the R4
   protobuf codec. Instances the codec cannot load (R4-removed
   `Patient.animal`, singleton JSON for 0..* `interpretation`) fall back to
   the JSON-path subset. The score records `r4Engine` as `fhirpath` or
   `json-path`.
3. **Semantic (R5)** — the same assertion expressions are evaluated with the
   JSON-path subset against the **converted** payload, not the gold file.
   `r5Engine` is always `json-path` because HAIStack has no R5 protobuf codec
   yet (`google_r5.go` is planned). This is not `pkg/fhirpath`.
4. **Information loss** — flags declared on the pair must appear in the loss
   list **detected by** `ConvertR4ToR5`. Declared flags are not copied into
   that list before the check.

HAIStack's production codec is R4 (`pkg/proto.GoogleR4Codec`). This corpus
does not require a live R5 protobuf codec; envelopes use canonical JSON.

## HL7 version conversion packages

HL7 publishes FHIR version conversion maps (for example
`hl7.fhir.uv.xver` / the core conversion maps). This corpus is **not** a
reimplementation of those maps. It is a small, citable test set focused on
four resource types (Patient, Observation, Condition, MedicationRequest) with
explicit information-loss flags so pipelines can be scored even when a
converter is lossy by design.
