# Track D — Terminology `$translate` consistency, map agreement, and provenance

Authored translation cases, a gold ConceptMap used as a **consistency**
check that `$translate` implements that map, a **divergent** in-repo
ConceptMap scored against the same cases (map-vs-cases agreement, not
translator or external-mapping quality), and an audit-backed provenance
model for `$translate`.

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
is a smaller, independently written map: it omits K and CBC-DIFF, maps WBC
as equivalent (cases want broad), and maps GLU (cases want unmatched). It
is not a one-field edit of the gold file. Accuracy 0.5 is that authored
error set, published as a class metric on this map versus `cases.json`.

| Class | Meaning |
|-------|---------|
| `exact` | equivalent / equal |
| `narrow` | source is broader than target (narrower result) |
| `broad` | source is narrower than target (wider result) |
| `unmatched` | no acceptable translation (unmatched, disjoint, noMap) |

## Metrics

Exact / narrow / broad / unmatched counts are `$translate` observed
classes from ConceptMap `equivalence` on the returned coding.

A case passes when `gotClass` matches authored gold and the target code
matches when gold specifies one.

The published command prints two objects:

| Key | Source | What it is |
|-----|--------|------------|
| `goldConsistency` | gold map × `cases.json` | Does `$translate` implement this map? **1.0 is expected.** No `byClass`. |
| `mapAgreement` | **divergent map** × `cases.json` | Class metric of this in-repo map vs authored labels. Accuracy 0.5 is the authored disagreement (omit K/CBC-DIFF, WBC equivalent, GLU mapped). **Not** translator quality and **not** an external mapping. |

**Accuracy** is the pass rate (`class` and `target` both match).

**Precision** and **recall** are published **only in `mapAgreement.byClass`**.
Precision is omitted when a class has no predictions; recall is omitted when
it has no gold support. A right class with a wrong target fails accuracy and
is not a class false positive.

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
