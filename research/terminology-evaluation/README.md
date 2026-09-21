# Track D — Terminology mapping quality and provenance

Evaluates ConceptMap translation quality against an **independent
gold-standard** catalogue and records **provenance** (ConceptMap version,
source CodeSystem version, timestamp) on each decision. Audit events are
emitted through `pkg/audit` so terminology outcomes join the same trail
as AI and view access.

The gold tuples are **not** generated from the map under evaluation. A
correct map can score F1 = 1; a defective map must score F1 < 1.

## Reproduce

```bash
make research-terminology
# or
go test ./research/terminology-evaluation
go run ./research/terminology-evaluation
```

The CLI prints both scores. It exits 0 when the gold ConceptMap
reproduces the catalogue and the pipeline ConceptMap does not (the
quality check is live). It does not treat pipeline F1 < 1 as failure.

## Gold set vs pipeline map

[`gold/translations.json`](gold/translations.json) lists expected
`(source, target, equivalence)` tuples for a synthetic vitals CodeSystem
(`https://example.org/CodeSystem/haistack-vitals`) onto LOINC. Those
tuples are the independent truth.

[`gold/conceptmap.json`](gold/conceptmap.json) is a **control** map that
implements the catalogue. Scoring it confirms the translator and scorer:
F1 = 1 here means Translate works, not that mapping quality was tested.

[`pipeline/conceptmap.json`](pipeline/conceptmap.json) is the map under
evaluation (version `1.1.0-defective`). It is the same canonical URL with
known defects so the scorer can report F1 < 1:

| Source | Gold | Pipeline defect |
|--------|------|-----------------|
| `hr` | LOINC 8867-4 (exact) | wrong target 9279-1 (false positive) |
| `dbp` | LOINC 8462-4 (exact) | element omitted (false negative) |
| `fever` | 8310-5 (broad / `wider`) | `equivalent` instead of `wider` (false positive) |
| `unknown` | unmatched / `noMap` | falsely mapped to 8867-4 (false positive) |

Expected pipeline confusion counts: TP = 8, FP = 3, FN = 1, F1 = 0.8.

Equivalence classes scored:

| Class | FHIR ConceptMap equivalence |
|-------|-----------------------------|
| exact | `equivalent`, `equal` |
| narrow | `narrower`, `specializes`, `source-is-broader-than-target` |
| broad | `wider`, `subsumes`, `source-is-narrower-than-target` |
| unmatched | no acceptable translation / `unmatched` |

Metrics: precision, recall, and F1 against the gold set, plus per-class
counts and per-tuple mismatches.

## Provenance model

Each **pipeline** translation records:

- ConceptMap canonical URL and version (`1.1.0-defective`)
- source CodeSystem URL and version
- target system
- translation timestamp
- equivalence class

Those fields are copied onto `pkg/audit` event details
(`terminology.translate`) so analytics and AI pipelines can cite which
map produced a code.

## Limitations

Finite ValueSet expansion in `pkg/terminology` does not replace a full
terminology server. This gold set is intentionally small and synthetic;
it does not redistribute SNOMED CT or UMLS subsets. The defective
pipeline map is a fixture for the scorer, not a recommended vitals map.
