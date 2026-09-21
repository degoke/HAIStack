# HAIStack research artefacts

This directory publishes **reproducible, vendor-neutral research artefacts**
that sit on top of HAIStack libraries. The code in `pkg/` already implements
policy-governed AI tools, ViewDefinitions, terminology projections, and
authorization. These artefacts demonstrate those capabilities with synthetic
data, scoring scripts, and citable methodology — without PHI and without
production LLM API keys.

## Research agenda

Recent FHIR literature highlights five high-impact gaps. HAIStack maps onto
them as follows:

| Gap | Track | Artefact |
|-----|-------|----------|
| Vendor-neutral FHIR benchmark suite | A | [`benchmarks/`](benchmarks/) |
| R4/R5/R6 semantic-conversion corpora | B | [`semantic-conversion/`](semantic-conversion/) |
| Computable consent / policy semantics | C | [`policy-semantics/`](policy-semantics/) |
| Terminology mapping quality and provenance | D | [`terminology-evaluation/`](terminology-evaluation/) |
| Reproducible FHIR → AI pipelines | E | [`ai-pipeline/`](ai-pipeline/) |

Priority for this release: **Track E** (product differentiation) and
**Track C** (SMART ecosystem need). Tracks A and D are included as runnable
P1 artefacts. Track B ships an R4→R5 corpus and scorer; a live R5 codec is
out of scope (see issue #11).

## How to reproduce

From the repository root (Go 1.26+):

```bash
make research                 # all tracks
make research-ai-pipeline     # Track E
make research-policy          # Track C
make research-conversion      # Track B
make research-terminology     # Track D
make research-benchmarks      # Track A
```

Each target runs the track's tests and a deterministic CLI (`go run`). No
network services, API keys, or PHI are required. The required **CI** workflow
(`.github/workflows/ci.yml`) runs `make research` as a blocking step. The
dedicated `.github/workflows/research.yml` workflow runs the same target.

## How to cite

See [`CITATION.cff`](../CITATION.cff) at the repository root.

```
Degoke Health AI Stack contributors. HAIStack: reproducible FHIR pipelines,
policy semantics, and evaluation corpora. 2026.
https://github.com/degoke/HAIStack
```

A Zenodo/figshare DOI can be added to `CITATION.cff` when the artefact is
deposited. Until then, cite the git tag or commit.

## FAIR metadata

| Principle | How this tree addresses it |
|-----------|----------------------------|
| Findable | `CITATION.cff`, this README, stable paths under `research/` |
| Accessible | Apache-2.0 git repository; no login for synthetic data |
| Interoperable | FHIR JSON, ViewDefinition, ConceptMap, SMART scopes, YAML scenarios |
| Reusable | Apache-2.0; synthetic-only; methodology docs per track; pinned seeds |

All datasets are synthetic. See [`LICENSE`](LICENSE).

## Provenance chain (Track E)

```
FHIR resources (validated)
  → ViewDefinition projection (pkg/view)
  → permissioned row set (pkg/auth)
  → AI tool invocation (pkg/ai)
  → stub model response
  → audit log + exportable provenance bundle (pkg/audit)
```

The provenance bundle records input hashes, view version, policy hash, tool
name, model adapter/seed, citations, and audit events.

## Relationship to packages

| Area | Path |
|------|------|
| AI tools | `pkg/ai` |
| Views | `pkg/view` |
| Audit | `pkg/audit` |
| Auth / policy | `pkg/auth` |
| SMART | `pkg/smart` |
| Terminology | `pkg/terminology`, `pkg/conceptmap` |
| Proto / codec | `pkg/proto`, `pkg/types` |
| Scenario runner | `pkg/testkit/authztest` (policy YAML; tests-only helper reused by Track C) |
| Evaluation store | `research/internal/researchutil.MemoryResourceStore` (Tracks A and E; not `pkg/testkit`) |

Related product issues: conformance IG pinning, authorization semantics,
Bulk Data / analytics paths, and proof-of-conformance benchmarks.
