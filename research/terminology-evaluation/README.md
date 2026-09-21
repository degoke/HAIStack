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

The published command dumps **`$translate` output** and the resolved
ConceptMap identity fields (`conceptMapUrl`, `conceptMapVersion`,
`sourceSystemVersion`). `cases.json` restates `conceptmap.json`, so that
dump is not translator quality and not an external mapping. There is no
published `implementsMap`, class-total, `accuracy`, or `consistency`
ratio, and no per-case `pass` rows. Tests still assert that `$translate`
matches authored cases (gold consistency) and class P/R on the known-error
map.

A case passes when `gotClass` matches authored gold and the target code
matches when gold specifies one.

**Precision** and **recall** are computed by `Evaluate` as one-vs-rest on
class labels (omitted when predicted or support is 0). A right class with
a wrong target fails the case and is not a class false positive. Those
scores are asserted in tests, not printed as a published artefact.

**Provenance** fields in the dump come from the `terminology.translate`
audit event **emitted by `pkg/terminology.Translate`**. They are copied
from the resolved ConceptMap body (`url`, `version`, `sourceUri` version).
A failed audit emit aborts the run. The harness stores the ConceptMap at
its FHIR `url` and calls `$translate` with that url (no version parameter).

## Finite ValueSet expansion

`pkg/terminology` expands only finite compose/expansion members. Unbounded
intensional ValueSets, missing global-catalog opt-in, and remote-only codes
are expected gaps: the gold set documents unmatched codes rather than
pretending local expansion is complete.
