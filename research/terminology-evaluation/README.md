# Track D — Terminology mapping quality and provenance

Evaluates ConceptMap translation quality against a **gold-standard**
synthetic map and records **provenance** (ConceptMap version, source
CodeSystem version, timestamp) on each decision. Audit events are emitted
through `pkg/audit` so terminology outcomes join the same trail as AI and
view access.

## Reproduce

```bash
make research-terminology
# or
go test ./research/terminology-evaluation
go run ./research/terminology-evaluation
```

## Gold set

[`gold/conceptmap.json`](gold/conceptmap.json) maps a synthetic vitals
CodeSystem (`https://example.org/CodeSystem/haistack-vitals`) onto LOINC.
[`gold/translations.json`](gold/translations.json) lists expected
`(source, target, equivalence)` tuples.

Equivalence classes scored:

| Class | FHIR ConceptMap equivalence |
|-------|-----------------------------|
| exact | `equivalent`, `equal` |
| narrow | `narrower`, `specializes`, `source-is-broader-than-target` |
| broad | `wider`, `subsumes`, `source-is-narrower-than-target` |
| unmatched | no acceptable translation / `unmatched` |

Metrics: precision, recall, and F1 against the gold set, plus per-class
counts.

## Provenance model

Each translation records:

- ConceptMap canonical URL and version
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
it does not redistribute SNOMED CT or UMLS subsets.
