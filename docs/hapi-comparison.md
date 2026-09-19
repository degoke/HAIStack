# HAIStack vs HAPI FHIR

Comparison of [HAIStack](https://github.com/degoke/HAIStack) against [HAPI FHIR](https://hapifhir.io/) (JPA server ~8.13 / docs 8.14). Dated 2026-09-19. Grounded in this repo’s packages and HAPI’s public docs, not marketing copy.

HAPI is a mature Java FHIR **library + complete JPA server**. Smile CDR is the commercial product on top of it. HAIStack is a **modular Go library stack** for composing FHIR-native stores, edge servers, offline sync, analytics, and permissioned AI access. They overlap on R4 REST, but they are not the same product.

Use this document to decide what to steal, what to skip, and where competing with HAPI is a trap.

---

## One-line verdict

HAPI already won “complete hospital FHIR server.” HAIStack should not try to become a Java-free HAPI. The durable advantage is **offline-first + SQL-on-FHIR + built-in SMART + AI tool policy**, on a composable Go runtime. The durable risk is shipping a REST surface that FHIR clients assume is HAPI-complete (vread, `$everything`, `_has`, Patient `$export`, referential integrity) and then failing those clients.

---

## Product shape

| | HAPI FHIR | HAIStack |
|---|---|---|
| Form | Java libraries + drop-in JPA server (or Plain Server facade over *your* backend) | Go libraries you compose; `cmd/haistack` is an operator CLI, not the product |
| Runtime | JVM, Spring, Hibernate, optional Lucene/Elasticsearch | One Go binary + SQLite or Postgres |
| FHIR versions | DSTU2, DSTU3, R4, R4B, R5 + version converters | R4 4.0.1 only (R5 proto stub) |
| Default job | Be a FHIR server | Be building blocks for local/edge/cloud/AI systems |
| Extensibility | Interceptor pointcuts across server + storage | Adapters, middleware, optional services; no general interceptor SPI |
| Maturity | ~12 years, production at national scale | Early-stage, active development |

---

## What HAPI does that we do not

These are real HAPI (or HAPI-JPA) capabilities that HAIStack either lacks, stubs, or only exposes as a host-supplied hook.

### FHIR REST completeness

Clients written against HAPI / public test servers expect this surface.

| Capability | HAPI | HAIStack |
|---|---|---|
| **vread** `GET /{type}/{id}/_history/{vid}` | Yes | No (instance `_history` list only) |
| Type / system `_history` | Yes | No |
| History `_since` / `_at` | Yes | No |
| Compartment URLs `Patient/{id}/{type}` | Yes | No (patient scope is auth post-filter, not a route) |
| **`$everything`** built-in | Yes (Patient/Encounter, paging, includes) | Route + SMART post-filter exist; **no built-in implementation** (needs `OperationService`) |
| Conditional create/update/delete | Yes, thoroughly | Yes |
| JSON Patch | Yes | Yes (RFC 6902) |
| **FHIR Patch** / XML Patch | Yes | No |
| Atomic `If-Match` | Yes | Partial (501 if store lacks atomic API) |
| `_include` / `_revinclude` `:iterate` | Yes | Direct includes only; wildcards rejected |
| OpenAPI / Swagger | Yes | No |
| Web tester overlay | Yes | No |
| Narrative generation | Yes | No |

### Search

HAPI JPA “fully implements most FHIR search.” Known HAPI holes (`_filter` partial, `_has` chains, `Location:near` as a box) are still far ahead of us.

| Capability | HAPI | HAIStack |
|---|---|---|
| `_has` / reverse chained | Yes (no chain *inside* `_has`) | Deferred |
| `_filter` | Partial | Deferred |
| Multi-hop chains | Yes | Single hop only |
| `uri` / `special` search types | Yes | Unsupported |
| Chained `_sort` | Yes (`patient.family`) | Registry fields + `_id` / `_lastUpdated` |
| Unique / combo SearchParameters | Yes | Standard registry SPs |
| Uplifted refchains | Yes (Smile extension) | No |
| Lucene / Elasticsearch full-text | Yes | Postgres FTS only; SQLite FTS unsupported; OpenSearch seam unused |
| `_pid`, `_compartmentLastUpdated` | HAPI extensions | No (and we should not copy `_pid`) |

SQLite search in HAIStack is basic `LookupMatch`. Advanced search is **Postgres-only**. HAPI’s H2/Postgres/MSSQL backends all run the same JPA search engine.

### Validation and integrity

| Capability | HAPI | HAIStack |
|---|---|---|
| HL7 instance validator | Yes (`org.hl7.fhir.validation`) | Built-in structural + profile; not the HL7 validator |
| FHIRPath invariants | Yes | Opt-in, best-effort |
| **Referential integrity** on write | Yes (configurable) | Syntactic reference checks only |
| Repository-validating interceptor | Yes (reject non-conformant writes) | Optional validator in the write path |
| Schematron | Yes | No |

This is one of the highest-leverage HAPI gaps. Apps that POST a bad `Observation.subject` to HAPI get a 400. On HAIStack they can persist.

### Terminology

| Capability | HAPI | HAIStack |
|---|---|---|
| `$lookup` / `$expand` / `$validate-code` | Yes | Yes (when wired) |
| **HTTP `ConceptMap/$translate`** | Yes | Library-only (`LocalService.Translate`); **no HTTP route** |
| `$subsumes` | Yes | No |
| SNOMED / LOINC / ICD **distribution upload** | CLI `upload-terminology` | Generic remote FHIR terminology client; no RF2/LOINC loaders |
| CodeSystem delta add/remove | Yes | No |
| ValueSet pre-expansion | Yes | Yes (finite VS + pre-expand jobs) |
| External large CS not stored in resource body | Yes (dedicated TRM_* tables) | Projection tables; not tuned for SNOMED-scale |

### Bulk data and batch

| Capability | HAPI | HAIStack |
|---|---|---|
| System `$export` | Yes | Yes |
| Group `$export` | Yes | Yes |
| **Patient `$export`** (instance / type) | Yes | **No** |
| `$export` + MDM expansion | Yes | No MDM |
| **`$import`** (NDJSON in) | Yes | **No** |
| `$hapi.fhir.bulk-patch` | HAPI-specific | No |
| `$expunge` | Yes | No |

### Subscriptions

| Capability | HAPI | HAIStack |
|---|---|---|
| R4 criteria (`Patient?name=…`) | Yes | Resource type only; advanced search criteria rejected |
| rest-hook | Yes | Yes |
| **WebSocket / email** | Yes | Explicitly not |
| R5 `SubscriptionTopic` | Yes | No |
| `$trigger-subscription` | Yes | No |

### Clinical reasoning, CDS, SDC extras

HAPI’s Clinical Reasoning module is a product of its own.

| Capability | HAPI | HAIStack |
|---|---|---|
| CQL engine | Yes | Inject `CQLProvider`; **no engine** |
| `$evaluate-measure`, care gaps, PlanDefinition `$apply` | Yes | Absent |
| CDS Hooks | Yes (`hapi-fhir-server-cds-hooks`) | Absent |
| `Questionnaire/$populate` + `$extract` | Yes (CQL + Observation/Definition extract) | Yes (FHIRPath/SDC; StructureMap extract) |
| `StructureDefinition/$questionnaire` | Yes | No |
| CRMI `$package` / `$data-requirements` | Yes | `$package` is **501** |
| `$assemble` | Not on HAPI’s CR questionnaire page | **Yes** |
| Adaptive `$next-question` | Not on HAPI’s CR questionnaire page | **Partial** (engine + HTTP; app supplies session adapter) |

### Platform features we do not have

- **Interceptor SPI** — HAPI’s real extension model (`SERVER_*` and `STORAGE_*` pointcuts). Auth, audit, consent, binary offload, subscriptions, and version conversion are interceptors.
- **AuthorizationInterceptor + ConsentInterceptor + SearchNarrowingInterceptor** — compartment RuleBuilder, optional FHIR query filters on instance ops, bulk-export rules, tenant-scoped rules.
- **BALP** audit pattern.
- **MDM** — golden records, match/link, `$member-match`.
- **Partitioning** — first-class multi-tenant partitions, including DB-partition mode.
- **IPS** generation.
- **GraphQL**.
- **`$diff`, `$meta` / `$meta-add` / `$meta-delete`, `$lastn`, `$document`, `$process-message`, `$snapshot`**.
- **FHIR version conversion** interceptor.
- **Plain Server** — FHIR REST facade over an arbitrary existing clinical DB.
- **Android client**, JAX-RS server, narrative, custom Java structures.
- **Mature CLI**: `run-server`, `upload-examples`, `upload-terminology` (SNOMED RF2, LOINC, ICD).
- **Clustering** as a Hibernate/JPA deployment pattern.

---

## What we do that HAPI does not

HAPI is not trying to be an offline edge runtime or an SQL-on-FHIR analytics engine. These are actual HAIStack leads, not “we also have a package.”

### Offline-first and sync

HAPI JPA is an always-online server. There is no Git-style device branch, outbox, or FHIR-aware merge.

HAIStack has:

- Embedded **SQLite** store (pure Go, WAL, migrations) as a first-class FHIR node.
- **`pkg/sync`**: local write → outbox → push to Postgres hub → pull canonical events → inbox idempotency.
- **`pkg/conflict`**: detect, auto-merge safe lists, FHIR Patch rebase artifacts.

This is the product thesis. HAPI will not grow this without becoming a different system.

### SQL-on-FHIR and analytics

HAPI’s analytics story is **HFQL**, a proprietary SQL dialect + JDBC driver, documented as unoptimized. It does not implement HL7 SQL-on-FHIR ViewDefinition operations.

HAIStack has:

- ViewDefinition v2 execution (`forEach`, nested select, `unionAll`, `resolve()`, `memberOf()`)
- `$viewdefinition-run`, `$viewdefinition-export`, `$materialize`, `$sqlquery-run`
- Incremental watermarks / CDC refresh
- **Parquet-on-FHIR** nested layout plus flat view Parquet
- Lakehouse / warehouse / manifest sinks

This is the other product thesis. Do not replace it with HFQL-style proprietary SQL.

### Built-in SMART / OAuth

HAPI is a resource server. SMART launch and tokens usually come from Keycloak, Smile CDR, or a custom interceptor. There is no first-class authorization server in open-source HAPI.

HAIStack has:

- `pkg/smart`: SMART 1.x + 2.2 CRUDS scopes, filters, backend-service assertions
- `pkg/oauth`: auth code + PKCE, refresh, client_credentials, revoke, JWKS, optional DCR, consent, EHR launch UI
- Layered authz: scope gate → policy DSL → patient compartment → SMART 2.2 query filters
- In-repo Inferno-style smoke tests

Launch orchestration and full Inferno Docker kits are still non-goals / partial.

### Policy-governed AI access

HAPI has no AI tool harness. You would put an LLM in front of the REST API and hope AuthorizationInterceptor holds.

HAIStack `pkg/ai` exposes typed tools (`read_fhir_resource`, `search_fhir_resources`, `run_view`, `write_fhir_resource`) with allow-lists, citations, audit, and optional human approval. That is a different threat model than “chatbot with a FHIR token.”

### Modular Go composition

HAPI modules exist, but the JPA server is still a Spring application. HAIStack lets you import `pkg/fhirpath` or `pkg/sqlite` alone. Edge mode is one binary + one Postgres. No Hibernate, no Lucene required.

### SDC assembly and adaptive forms

HAPI CR covers populate/extract (and CQL). HAIStack covers **`$assemble`** and adaptive session contracts, plus a renderer-neutral field state model. Extraction is application-pluggable (including StructureMap) and does not auto-commit the resulting transaction Bundle.

### Terminology as rebuildable projections

Both persist terminology beside resources. HAIStack’s model is explicit: `CodeSystem` / `ValueSet` stay ordinary FHIR resources; `pkg/terminology` compiles tenant-scoped projections and validation is opt-in. That is a better fit for edge/offline than HAPI’s “upload SNOMED into TRM_* or else.” It does **not** yet replace HAPI’s large-codesystem loaders.

---

## What we can do better (steal the pattern, not the stack)

Ranked by leverage for an R4 edge/AI server that FHIR clients will actually trust.

### 1. Close the REST “obviously missing” set

These are cheap compared to MDM/CQL and they unblock real clients:

- **vread**
- History `_since` / `_at` (instance first, then type)
- Built-in **Patient `$everything`** (compartment walk + paging)
- **Patient `$export`**
- HTTP **`ConceptMap/$translate`** (logic already exists)
- FHIR Patch (`application/fhir+json` patch semantics)
- Stop advertising operations that 501 (`$package` until it exists)

Do this. This is table stakes, not “becoming HAPI.”

### 2. Search that SMART apps and EHRs actually issue

- `_has`
- `uri` search type
- `_include=*` / recursive includes (even if capped)
- Chain depth 2 for the common `Encounter.subject.name` class of queries

Do not start with Lucene, uplifted refchains, or `_filter`. HAPI itself only partially implements `_filter`.

### 3. A small interceptor / hook SPI

HAPI’s interceptors are why people can add auth, consent, tenant routing, and binary offload without forking the server. We have `AuthMiddleware`, core Validator/Indexer/Outbox hooks, and a sync conflict handler. That is not an ecosystem.

A Go-sized version: ordered hooks at incoming request, pre-storage create/update/delete, post-commit, outgoing response. Auth, audit, subscriptions, and binary externalization should sit on those hooks instead of growing special cases.

Do this, but **do not** clone the full `Pointcut` enum.

### 4. Referential integrity and validator honesty

Optional `RepositoryValidating`-style “refuse the write” is already in the architecture; make reference *existence* a configurable write check. Keep terminology validation opt-in (correct for offline). Document fast vs full vs HAPI-instance-validator parity so we do not pretend we run the HL7 validator.

### 5. Authorization as compartments + search narrowing

We already have a stronger *identity* story than open-source HAPI (built-in OAuth). Steal HAPI’s *authorization* shape:

- RuleBuilder-style **compartment allow/deny** (we post-filter; HAPI can deny before work)
- **Search narrowing** (rewrite the query, do not fetch-then-drop) — we do SMART 2.2 filter intersection; generalize it
- Consent as a response filter (we already post-filter `$everything` / includes)

HAPI warns that fetch-then-hide still spends the query. We have the same issue.

### 6. Subscriptions with real criteria

Upgrade R4 `Subscription.criteria` from resource type to the search parser we already have. rest-hook stays the only channel until someone needs websocket.

### 7. `$import` and operator CLI

HAPI’s CLI is how people load LOINC and example data. We need `$import` (or a first-class NDJSON ingest) and CLI backup/restore more than we need `hapi-fhir-cli run-server` (we already have `haistack serve`).

### 8. CapabilityStatement that matches the binary

HAPI’s `/metadata` is not perfect, but it is the contract. Ours advertises ops that 501 without services. Clients (and Inferno) will punish that.

---

## What we cannot / should not do better

Copying these is how a modular edge stack becomes a worse HAPI, years late.

| HAPI thing | Why not |
|---|---|
| **DSTU2 / DSTU3 / R5 as a goal** | Version converters are a decade of HL7 work. Stay R4 until a customer brings R5 data. |
| **HL7 instance validator in-process** | It is a Java world. Call it as an optional sidecar later; do not reimplement. |
| **Full CQL / Measure / care-gap engine** | Separate product. Keep `CQLProvider` injectable. |
| **CDS Hooks** | Only if we are building an EHR CDS host. Not required for the store/sync/AI thesis. |
| **MDM / golden records** | A master-data product. Our conflict package is for **device sync**, not enterprise matching. |
| **JPA-style partitioning / clustering** | Postgres tenant scoping is enough for edge. National-scale partition routing is Smile’s job. |
| **HFQL** | We already have the standard (ViewDefinition). Do not add a proprietary SQL dialect. |
| **GraphQL / FHIRCast / IPS** | Useful later; none of them differentiate vs HAPI, and IPS is a profile pack on `$everything`. |
| **Lucene/Elasticsearch** | Finish Postgres search first. OpenSearch seam can wait. |
| **SNOMED RF2 loader** | Partner with a terminology service (Ontoserver, tx.fhir.org, Snowstorm). Do not ingest RF2 in-process unless a deployment cannot network. |
| **Android client** | We are Go. Mobile should use the local SQLite runtime + sync, not a HAPI Android port. |
| **Interceptor-everything culture** | HAPI’s power and its complexity. A short hook list, not 80 pointcuts. |
| **Drop-in “complete FHIR server” positioning** | Firely, Aidbox, Smile, Google Healthcare API, Azure FHIR already occupy that box. |

HAPI also has things that look like gaps but are **intentional non-goals for us**: being a facade over Epic/Cerner tables (Plain Server), JAX-RS, Spring Boot ops, H2-in-process demos.

---

## Honest overlap (both have a version)

Do not treat these as differentiators.

| Area | Both have | Catch |
|---|---|---|
| CRUD + transactions/batches | Yes | We lack vread / history filters |
| `/metadata` | Yes | Ours is partial and sometimes dishonest |
| SMART resource-server scopes | Yes (HAPI via interceptors) | We also *issue* tokens |
| Bulk `$export` | Yes | We lack Patient-level and `$import` |
| `$validate` | Yes | Different engines; HAPI’s is the ecosystem default |
| `$populate` / `$extract` | Yes | Different engines (CQL vs FHIRPath/SDC) |
| Subscriptions rest-hook | Yes | Ours cannot express search criteria |
| Multi-tenancy | Yes | HAPI partitions; we `Tenant(id)` on Postgres |
| Jobs / batch | Yes (HAPI batch2) | We have `pkg/jobs` |
| Binary / attachments | Yes | We defer S3 multipart, signed URLs, retention |
| FHIR NPM / IG install | Yes | Module v1 does not execute views/permissions/subscriptions declared in the manifest |
| FHIRPath | Yes | Ours is Verily-backed, one resource per eval unless `resolve` is configured |

---

## Suggested build order (if this comparison drives work)

1. **Trust the REST surface:** vread, history `_since`, `$everything`, Patient `$export`, ConceptMap `$translate` HTTP, CapabilityStatement accuracy.
2. **Search EHR-apps actually send:** `_has`, `uri`, include wildcards, 2-hop chains.
3. **Hooks + referential integrity + search narrowing.**
4. **Subscription criteria; `$import`; CLI backup/restore.**
5. Stop. Do not start MDM, CQL, R5, or Elasticsearch unless a named deployment requires them.

What to keep investing in (this is not a HAPI catch-up list):

- Sync + conflict for real transports (HTTP is already there; harden it)
- SQL-on-FHIR typed parameters (`valueReference` / `valueCode`, not only `valueString`)
- AI tool policy + audit
- SQLite advanced search so offline nodes are not second-class
- Module installer actually applying declared views / permissions / subscriptions

---

## Sources

HAIStack: package READMEs under `pkg/*/`, `docs/sql-on-fhir-gap.md`, `docs/smart-auth-architecture.md`, HTTP router/handlers.

HAPI: [docs index](https://hapifhir.io/hapi-fhir/docs/introduction/), [JPA search](https://hapifhir.io/hapi-fhir/docs/server_jpa/search.html), [terminology](https://hapifhir.io/hapi-fhir/docs/server_jpa/terminology.html), [questionnaires / CR](https://hapifhir.io/hapi-fhir/docs/clinical_reasoning/questionnaires.html), [authorization interceptor](https://hapifhir.io/hapi-fhir/docs/security/authorization_interceptor.html), [HFQL](https://hapifhir.io/hapi-fhir/docs/hfql/hfql.html), [CLI](https://hapifhir.io/hapi-fhir/docs/tools/hapi_fhir_cli.html), [starter](https://github.com/hapifhir/hapi-fhir-jpaserver-starter), JPA `JpaConstants` operations list.
