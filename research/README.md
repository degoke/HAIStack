# HAIStack research artefacts

Vendor-neutral, reproducible artefacts that connect HAIStack libraries to
documented FHIR / clinical-AI research gaps. **All datasets are synthetic.
No PHI is present or required.**

Cite this repository with [`CITATION.cff`](../CITATION.cff) at the repo root.
Dataset license notes live in [`LICENSE`](./LICENSE).

```bash
make research
```

Each track also has its own target and `go test` entry point. Commands are
deterministic: fixed clocks, fixed seeds, and a stub model that never calls a
network LLM.

## Research agenda

HAIStack's libraries already implement pieces of the research-grade story
(policy-governed AI tools, ViewDefinitions, terminology projections, SMART
scope adapters, audit events). This tree turns those libraries into citable,
reproducible artefacts rather than leaving the evidence implicit.

| Track | Directory | Literature gap | HAIStack packages |
|-------|-----------|----------------|-------------------|
| A | [`benchmarks/`](./benchmarks) | Vendor-neutral FHIR benchmark suite | `pkg/store`, `pkg/view`, `research/internal/memstore` |
| B | [`semantic-conversion/`](./semantic-conversion) | R4/R5 semantic-conversion corpora | `pkg/types`, `pkg/fhirpath`, `pkg/proto` (R4 codec; R5 planned) |
| C | [`policy-semantics/`](./policy-semantics) | Computable consent / policy semantics | `pkg/auth`, `pkg/smart`, `pkg/testkit/authztest` |
| D | [`terminology-evaluation/`](./terminology-evaluation) | `$translate` provenance identity; class metrics in tests | `pkg/terminology`, `pkg/conceptmap`, `pkg/audit` |
| E (P0) | [`ai-pipeline/`](./ai-pipeline) | Reproducible FHIR → AI pipelines | `pkg/ai`, `pkg/view`, `pkg/auth`, `pkg/audit`, `pkg/validate` |

Tracks E and C are P0 (product differentiation and SMART ecosystem need).
Tracks A and D are P1. Track B is P2: the corpus starts at R4→R5 and does not
claim full R6 conversion.

## How to reproduce

From the repository root (Go 1.26+):

| Target | What it runs |
|--------|----------------|
| `make research-ai-pipeline` | Track E: FHIR → view → policy → AI tool → seeded stub → provenance bundle |
| `make research-policy` | Track C: policy-semantics catalogue (≥10 scope ∩ policy examples) |
| `make research-conversion` | Track B: ≥50 paired R4/R5 instances + scoring |
| `make research-terminology` | Track D: Translate-resolved ConceptMap identity (class P/R in tests) |
| `make research-benchmarks` | Track A: seeded datasets + portable workloads + HAIStack runner |
| `make research` | All of the above |

CI workflow [`.github/workflows/research.yml`](../.github/workflows/research.yml)
runs `go test ./research/...` as a blocking job and `make research-run`
(published `go run` commands only) as a second blocking job. Local
`make research` still runs tests then commands per track.

## FAIR metadata

| Principle | How these artefacts comply |
|-----------|----------------------------|
| Findable | Track READMEs, `CITATION.cff`, stable scenario/pair IDs |
| Accessible | Apache 2.0 in this public repository; no API keys |
| Interoperable | FHIR JSON, ViewDefinition, ConceptMap, SMART scopes, portable YAML workloads |
| Reusable | Synthetic data, documented methodology, deterministic seeds, provenance hashes |

Zenodo/figshare DOIs are a publishing step, not part of this issue. The
citation file is the in-repo handle until a DOI is minted.

## What this is not

- Academic paper writing (artefacts enable papers; they are not papers)
- Real PHI or licensed clinical code systems (gold maps use synthetic codes)
- Production LLM API integration in CI (Track E uses a seeded stub)
- Full FHIR R6 conversion or a HAPI horse-race

## Related product work

- Conformance / IG pinning for stricter AI-pipeline validation
- Authorization scenario catalogue in `pkg/testkit/authztest`
- Bulk Data + analytics paths that can feed Track A and E later
- Benchmark / proof-of-conformance overlap with Track A
