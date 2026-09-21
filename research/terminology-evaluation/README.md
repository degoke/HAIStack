# Track D — Terminology `$translate` consistency and provenance

Authored translation cases, a gold ConceptMap used as a **consistency**
check that `$translate` implements that map, and an audit-backed
provenance model for `$translate`. Class precision/recall is exercised in
tests against a known-error map; it is not a published quality number.

## Reproduce

```bash
make research-terminology
go test ./research/terminology-evaluation -count=1
go run ./research/terminology-evaluation/cmd
```

## Gold set

[`testdata/conceptmap.json`](./testdata/conceptmap.json) is a synthetic
ConceptMap (`http://haistack.dev/research/ConceptMap/lab-to-panel|1.0.0`)
from a toy lab CodeSystem to a toy panel CodeSystem. Codes are **not**
SNOMED CT or LOINC — they exist so the artefact can be redistributed without
terminology licenses.

[`testdata/cases.json`](./testdata/cases.json) is the **authored** expected
equivalence class and target.

[`testdata/divergent-conceptmap.json`](./testdata/divergent-conceptmap.json)
is a known-error fixture for class-metric unit tests (omit K/CBC-DIFF, WBC
equivalent, GLU mapped). It is not published by the command and is not an
evaluation of the translator or of an external mapping.

| Class | Meaning |
|-------|---------|
| `exact` | equivalent / equal |
| `narrow` | source is broader than target (narrower result) |
| `broad` | source is narrower than target (wider result) |
| `unmatched` | no acceptable translation (unmatched, disjoint, noMap) |

## Metrics

The published command prints **gold consistency** only: gold map ×
`cases.json`. **1.0 is expected** (`$translate` implements this map). That
is not translator quality and not an external mapping.

Exact / narrow / broad / unmatched counts are `$translate` observed
classes from ConceptMap `equivalence` on the returned coding.

A case passes when `gotClass` matches authored gold and the target code
matches when gold specifies one.

**Consistency** is the pass rate (`class` and `target` both match). The
published JSON key is `consistency`, not `accuracy`. **1.0 is expected.**

**Precision** and **recall** are computed by `Evaluate` as one-vs-rest on
class labels (omitted when predicted or support is 0). A right class with
a wrong target fails consistency and is not a class false positive. Those
scores are asserted in tests, not printed as a published artefact.

**Provenance completeness** is the share of translations whose
`terminology.translate` audit event was **emitted by
`pkg/terminology.Translate`**. The harness stores the ConceptMap at its FHIR
`url` and calls `$translate` with that url (no version parameter). Audit
`conceptMapUrl` / `version` / `sourceUri` version come from the resolved
ConceptMap body. Published provenance 1.0 means `conceptmap.json` includes
those fields; that is the metric working. A body missing them scores 0. A
failed audit emit aborts the run.

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
