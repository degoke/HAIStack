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
| Terminology mapping quality and provenance | D | [`terminology-evaluation/`](terminology-evaluation/) — planted scorer fixture, not a quality study |
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
network services, API keys, or PHI are required. Pull request CI is the
required **CI** workflow (`.github/workflows/ci.yml`), which runs
`make research` inside the `ci` job. Merge blocking depends on that GitHub
check remaining required in branch protection; that setting cannot be
encoded in repository files. There is no separate research workflow.

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
  → authorized view/tool execution (pkg/auth; full authorized view output,
    not per-row ACL filtering)
  → AI tool invocation (pkg/ai)
  → stub model response
  → audit log + exportable provenance bundle (pkg/audit)
```

The provenance bundle records input hashes, view name/version, policy hash,
tool **name** (not a separate tool-version field), model adapter/seed,
citations, and audit events.

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

## Scope relative to issue #11

This tree implements the issue **acceptance criteria** (reproducible
tracks, ≥10 SMART ∩ policy examples, ≥50 conversion pairs, terminology
gold + metrics harness, `CITATION.cff`, AI provenance chain). It does not
implement every sentence in the issue **proposal**:

- Consent state is compiled into YAML policy/overlay; `scenarios.yaml`
  has no consent-state field.
- Track E records the tool **name**, not a separate tool version.
- Track B is a catalogue scorer, not a live R5 converter.
- Track D is a planted scorer fixture, not a mapping-quality study.

