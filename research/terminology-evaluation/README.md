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
`cases.json`. `implementsMap` is true when every case matches `$translate`
(`failed` is 0). That is expected when cases restate this map. It is not
translator quality and not an external mapping. There is no published
`accuracy` or `consistency` ratio, and the command does not print
`passed`/`failed` counts or per-case `pass` rows.

Exact / narrow / broad / unmatched counts are `$translate` observed
classes from ConceptMap `equivalence` on the returned coding.

A case passes when `gotClass` matches authored gold and the target code
matches when gold specifies one.

**Precision** and **recall** are computed by `Evaluate` as one-vs-rest on
class labels (omitted when predicted or support is 0). A right class with
a wrong target fails the case and is not a class false positive. Those
scores are asserted in tests, not printed as a published artefact.

**Provenance** is complete when each `terminology.translate` audit event
was **emitted by `pkg/terminology.Translate`** and the resolved ConceptMap
body has `url`, `version`, and `sourceUri`. The published flag
`provenanceComplete` is that boolean (true when `conceptmap.json` includes
those fields). A body missing them is false. A failed audit emit aborts
the run. The harness stores the ConceptMap at its FHIR `url` and calls
`$translate` with that url (no version parameter).

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
