# Track D — Terminology mapping quality and provenance

Scores ConceptMap translations against an **independent gold-standard**
catalogue and records **provenance** (ConceptMap version, source
CodeSystem version, timestamp) on each decision. Audit events are
emitted through `pkg/audit` so terminology outcomes join the same trail
as AI and view access.

This is **not** a mapping-quality study of a real translator. The
defective map is a **planted scorer fixture** so the metrics code can
report F1 < 1. A separately authored map would be required for a genuine
quality evaluation.

## Reproduce

```bash
make research-terminology
# or
go test ./research/terminology-evaluation
go run ./research/terminology-evaluation
```

The CLI prints both scores.

- Exit 0: gold control is perfect (Translate + scorer harness) **and**
  the defective fixture is not (scorer can emit F1 < 1).
- Exit 1: harness failure (gold control not F1 = 1) **or** the fixture
  accidentally matches gold (scorer cannot demonstrate F1 < 1).
- Fixture F1 < 1 is success for the harness, not a mapping-quality
  result.

## Gold set vs scorer fixture

[`gold/translations.json`](gold/translations.json) lists expected
`(source, target, equivalence)` tuples for a synthetic vitals CodeSystem
(`https://example.org/CodeSystem/haistack-vitals`) onto LOINC. Those
tuples are the independent truth.

[`gold/conceptmap.json`](gold/conceptmap.json) is a **control** map that
implements the catalogue. Scoring it confirms the translator and scorer:
F1 = 1 here means Translate works, not that mapping quality was tested.

[`fixture/defective-conceptmap.json`](fixture/defective-conceptmap.json)
is a planted map (version `1.1.0-defective`) with known errors so the
scorer can report F1 < 1:

| Source | Gold | Planted defect |
|--------|------|----------------|
| `hr` | LOINC 8867-4 (exact) | wrong target 9279-1 (false positive) |
| `dbp` | LOINC 8462-4 (exact) | element omitted (false negative) |
| `fever` | 8310-5 (broad / `wider`) | `equivalent` instead of `wider` (false positive) |
| `unknown` | unmatched / `noMap` | falsely mapped to 8867-4 (false positive) |

Planted fixture confusion counts: TP = 8, FP = 3, FN = 1, F1 = 0.8.

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

Each **fixture** translation records:

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
it does not redistribute SNOMED CT or UMLS subsets. The defective map is
a scorer fixture, not a recommended vitals map and not an independently
produced mapping.
