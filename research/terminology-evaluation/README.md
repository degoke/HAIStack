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
expected equivalence class:

| Class | Meaning |
|-------|---------|
| `exact` | equivalent / equal |
| `narrow` | source is broader than target (narrower result) |
| `broad` | source is narrower than target (wider result) |
| `unmatched` | no acceptable translation (unmatched, disjoint, noMap) |

## Metrics

Exact / narrow / broad / unmatched counts are the **translator's** observed
class (`gotClass` from `$translate` + ConceptMap equivalence), not the gold
label. A case passes when `gotClass` matches gold and the target code matches
when gold specifies one.

**Precision** is TP / (TP + FP) among predicted matches (`gotClass` other
than unmatched). **Recall** is TP / (TP + FN) among gold matches. These are
not the overall pass rate.

**Provenance completeness** is the share of attempted translations whose
emitted `terminology.translate` audit event records ConceptMap URL+version,
source CodeSystem version, and timestamp. Gold-file fields alone do not
count; a failed `LogTerminologyTranslate` aborts the run.

Each translation is appended as a `terminology.translate` audit event
(`pkg/audit.LogTerminologyTranslate`).

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
