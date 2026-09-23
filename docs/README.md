# HAIStack documentation

This directory is the entry point for project documentation. The [root README](../README.md) stays the quick orientation; these guides go deeper on architecture and composition.

## Start here

| Guide | What you will learn |
|-------|---------------------|
| [Overview](overview.md) | What HAIStack is, why it exists, core ideas, and project status |
| [Architecture](architecture.md) | Layer diagram, write path, transactional vs analytical paths |
| [Composition patterns](composition-patterns.md) | Which packages to combine for offline, edge, cloud, SDC, AI, and analytics workloads |
| [Examples](../examples/README.md) | Runnable `go run ./examples/...` programs |
| [CLI](../cmd/haistack/README.md) | `haistack` commands, config, and environment variables |
| [Conformance](../conformance/README.md) | FSH authoring, IG build, and validator fixtures |
| [Research](../research/README.md) | Synthetic benchmarks and evaluation artefacts |

## Package reference

Each library under `pkg/` has a dedicated README: purpose, ecosystem role, usage modes, examples, and limits. Import path: `github.com/degoke/haistack/pkg/<name>`.

### Foundation

| Package | README | Role |
|---------|--------|------|
| `types` | [README](../pkg/types/README.md) | FHIR JSON envelopes, normalization, hashing |
| `proto` | [README](../pkg/proto/README.md) | Optional Google FHIR R4 proto adapter |
| `store` | [README](../pkg/store/README.md) | Storage interfaces and `WriteSession` |
| `sqlite` | [README](../pkg/sqlite/README.md) | Embedded offline database |
| `postgres` | [README](../pkg/postgres/README.md) | Tenant-scoped server store |
| `registry` | [README](../pkg/registry/README.md) | FHIR definition catalog and snapshots |
| `terminology` | [README](../pkg/terminology/README.md) | CodeSystem/ValueSet projections and lookup |
| `validate` | [README](../pkg/validate/README.md) | Structural validation engine |

### FHIR runtime kernel

| Package | README | Role |
|---------|--------|------|
| `core` | [README](../pkg/core/README.md) | CRUD, history, transaction bundles |
| `search` | [README](../pkg/search/README.md) | Registry-driven search |
| `sync` | [README](../pkg/sync/README.md) | Outbox, push/pull, hub integration |
| `conflict` | [README](../pkg/conflict/README.md) | Resource-aware merge |
| `hooks` | [README](../pkg/hooks/README.md) | Pre/post storage and HTTP intercept SPI |
| `fhirpath` | [README](../pkg/fhirpath/README.md) | In-memory FHIRPath engine |
| `cql` | [README](../pkg/cql/README.md) | CQL 1.5 for SDC and measures |
| `sdc` | [README](../pkg/sdc/README.md) | Questionnaire population, validation, extraction |
| `structuremap` | [README](../pkg/structuremap/README.md) | StructureMap extraction engine |
| `conceptmap` | [README](../pkg/conceptmap/README.md) | ConceptMap translation for StructureMap |

### Documents, bulk data, and workflows

| Package | README | Role |
|---------|--------|------|
| `binary` | [README](../pkg/binary/README.md) | Blobs, Binary, DocumentReference |
| `export` | [README](../pkg/export/README.md) | FHIR Bulk Data `$export` |
| `bulkimport` | [README](../pkg/bulkimport/README.md) | FHIR Bulk Data `$import` |
| `subscriptions` | [README](../pkg/subscriptions/README.md) | Change-triggered workflows |
| `jobs` | [README](../pkg/jobs/README.md) | Background job runtime |
| `audit` | [README](../pkg/audit/README.md) | Audit event model and emit helpers |

### Views, analytics, and AI

| Package | README | Role |
|---------|--------|------|
| `view` | [README](../pkg/view/README.md) | ViewDefinition execution |
| `analytics` | [README](../pkg/analytics/README.md) | Reporting tables, CSV/Parquet refresh |
| `parquetfhir` | [README](../pkg/parquetfhir/README.md) | Parquet-on-FHIR nested layouts |
| `ai` | [README](../pkg/ai/README.md) | Policy-governed AI tool harness |

### Security and identity

| Package | README | Role |
|---------|--------|------|
| `auth` | [README](../pkg/auth/README.md) | Principals, policies, compartment checks |
| `smart` | [README](../pkg/smart/README.md) | SMART on FHIR scopes and launch |
| `oauth` | [README](../pkg/oauth/README.md) | Embeddable OAuth2/SMART authorization server |

### Composition, HTTP, and clients

| Package | README | Role |
|---------|--------|------|
| `modules` | [README](../pkg/modules/README.md) | Manifest-driven capability modules |
| `packages` | [README](../pkg/packages/README.md) | NPM package install from packages.fhir.org |
| `runtime` | [README](../pkg/runtime/README.md) | Runtime builder and lifecycle |
| `http` | [README](../pkg/http/README.md) | FHIR REST adapter |
| `client` | [README](../pkg/client/README.md) | Go SDK for REST, sync, SMART, bulk |

### Tooling and conformance helpers

| Package | README | Role |
|---------|--------|------|
| `testkit` | [README](../pkg/testkit/README.md) | Fixtures, fakes, authz scenarios |
| `conformance` | [README](../pkg/conformance/README.md) | IG validator wiring for CI |

## Suggested reading order

1. [Overview](overview.md) — vocabulary and goals  
2. [Composition patterns](composition-patterns.md) — pick a stack shape  
3. Package README for each import you use  
4. [Examples](../examples/README.md) — run the closest sample  
5. [CLI](../cmd/haistack/README.md) or [runtime](../pkg/runtime/README.md) — ship a process  

## Contributing to docs

- Keep **package-specific** detail in `pkg/<name>/README.md` (and `doc.go` package comments).  
- Keep **cross-cutting** narratives in `docs/`.  
- When you add a new `pkg/*` library, add a README using the same sections as existing packages: *What it does*, *When to use it*, *Usage*, *Where it fits*, *Limits*.
