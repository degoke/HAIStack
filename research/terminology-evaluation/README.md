# Track D — Terminology mapping quality and provenance

Authored translation cases, a gold ConceptMap used as a **consistency**
check, a held-out ConceptMap scored against those cases, and an
audit-backed provenance model for `$translate`.

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
equivalence class and target. Map URL and version are not taken from this
file.

[`testdata/heldout-conceptmap.json`](./testdata/heldout-conceptmap.json)
is a different map that labels WBC `equivalent` instead of `wider`.

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

A case passes when `gotClass` matches authored gold and the target code
matches when gold specifies one.

The published command prints two objects:

| Key | Source | What it is |
|-----|--------|------------|
| `goldConsistency` | gold map × `cases.json` | Pass rate. **1.0 means `$translate` implements this map**, not quality. No `byClass`. |
| `classMetrics` | **held-out map** × `cases.json` | Quality of a map that disagrees with authored labels (WBC). Accuracy 0.875; exact P=0.75 R=1; broad R=0. |

**Accuracy** is the pass rate (`class` and `target` both match).

**Precision** and **recall** are published **only in `classMetrics.byClass`**
(one-vs-rest on **class labels**). Precision is omitted when a class has no
predictions; recall is omitted when it has no gold support. A right class
with a wrong target fails accuracy and is not a class false positive.

**Provenance completeness** is the share of translations whose
`terminology.translate` audit event was **emitted by
`pkg/terminology.Translate`**. The harness stores the file under
`urn:haistack:research:eval-conceptmap` and calls `$translate` with that
key. Audit `conceptMapUrl` / `version` / `sourceUri` version come from the
**ConceptMap body**, not the store key. A body missing `url`, `version`, or
`sourceUri` scores provenance 0. A failed audit emit aborts the run.

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
