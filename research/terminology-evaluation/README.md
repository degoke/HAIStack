# Track D — Terminology mapping quality and provenance

Gold-standard ConceptMap plus quality metrics that grade **the translator**,
and an audit-backed provenance model for translations that affect validation
or analytics.

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

[`testdata/cases.json`](./testdata/cases.json) lists source codes and the
expected equivalence class. Map URL and version are **not** taken from this
file; they come from the ConceptMap `$translate` resolved.

| Class | Meaning |
|-------|---------|
| `exact` | equivalent / equal |
| `narrow` | source is broader than target (narrower result) |
| `broad` | source is narrower than target (wider result) |
| `unmatched` | no acceptable translation (unmatched, disjoint, noMap) |

## Metrics

Exact / narrow / broad / unmatched counts are the translator's observed
class. `$translate` returns target `Coding` values that include
`equivalence` from the ConceptMap match; `gotClass` is derived from that
field.

A case passes when `gotClass` matches gold and the target code matches when
gold specifies one.

The published command prints two objects:

| Key | Source | What it is |
|-----|--------|------------|
| `goldConsistency` | [`testdata/cases.json`](./testdata/cases.json) | Pass rate against the gold map. **1.0 means `$translate` implements this map**, not an independent quality signal. `byClass` is omitted here because every class is perfect for that reason. |
| `classMetrics` | [`testdata/mismatch-cases.json`](./testdata/mismatch-cases.json) | Same map, **wrong labels**. Accuracy 0.75; `byClass` exact P=1 R=0.75, broad P=0. These are the multi-class numbers. |

**Accuracy** is the pass rate (`class` and `target` both match).

**Precision** and **recall** are published **only in `classMetrics.byClass`**
(one-vs-rest on **class labels**). A right class with a wrong target fails
accuracy and does not count as a class false positive. There is no top-level
precision/recall.

**Provenance completeness** is the share of translations whose
`terminology.translate` audit event was **emitted by
`pkg/terminology.Translate`** from the ConceptMap **body** it resolved (`url`,
`version`, `sourceUri` version, timestamp). The harness stores the map under
its `url` and calls `$translate` **without a version parameter**; audit version
comes from the resource JSON, not a harness lookup key. It does not call
`LogTerminologyTranslate`. `cases.json` does not publish map URL, version, or
source-system version. A map missing `url` is rejected; a map missing
`version`/`sourceUri` scores provenance 0. A failed audit emit aborts the run.

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
