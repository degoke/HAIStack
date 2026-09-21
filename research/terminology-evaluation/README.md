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

**Accuracy** is the pass rate (`passed / (passed + failed)`). It is 1.0
on the published gold set when `$translate` matches every case — that is
the translator implementing the map, not a separate quality headline.

**Precision** and **recall** are published **only in `byClass`**
(one-vs-rest: precision over predictions of that class, recall over gold
support). There is no top-level precision/recall: under micro-average
every error is both FP and FN, so those figures collapse to accuracy, and
on a complete gold set they stay 1.0 for the same reason the pass rate
does.

**Provenance completeness** is the share of translations whose
`terminology.translate` audit event was **emitted by
`pkg/terminology.Translate`** from the ConceptMap it resolved (`url`,
`version`, `sourceUri` version, timestamp). The harness loads the map
into the terminology store and calls `$translate`; it does not call
`LogTerminologyTranslate` or copy gold-file identity into the event.
`cases.json` does not publish map URL, version, or source-system version.
A map missing `url` is rejected; a map missing `version`/`sourceUri`
scores provenance 0. A failed audit emit aborts the run.

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
