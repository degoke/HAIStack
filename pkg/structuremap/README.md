# haistack-structuremap (`pkg/structuremap`)

Minimal FHIR **StructureMap** engine for **SDC questionnaire extraction**.

Executes a practical subset of Mapping Language transforms against in-memory maps. Full HAPI StructureMap parity is **not** a goal.

---

## What it does

**`Engine.Execute(ctx, Map, ExecuteInput)`** (`engine.go`):

1. Pick **entry group** — `StructureMap.name` match, else first group
2. Bind inputs (`questionnaire`, `questionnaireResponse`, `src`, `q`, …)
3. Run rules: source binding (FHIRPath conditions), targets, nested rules, **dependent** groups
4. Return `[]json.RawMessage` for target outputs (bundle-ready resources)

**Transforms** (`transforms.go`, tests in `transforms_test.go`):

`copy`, `create`, `uuid`, `cc`, `c`, `evaluate`, `translate`, `id`, `reference`, `pointer`, `append`, `cast`, `truncate`, `escape`, `qty`, `cp`, `dateOp`, literals (`boolean`, `integer`, …), and related helpers.

**`Engine.Strict`:** required sources with no match → error (runtime sets `true`).

**`CardinalityResolver`:** repeating vs singular JSON arrays (`TestPatientNameIsArray`, `TestObservationCodeIsNotArray`).

**SDC wiring** (`sdc.go`):

- `NewExtractor(Config)` → `sdc.StructureMapExtractor`
- `ExtractorRun` loads map via `Resolver.Resolve(ctx, q.SourceStructureMap)`

**Resolvers:** `StoreResolver` (ResourceStore + DefinitionStore), `StaticResolver` (tests).

Uses [`pkg/conceptmap`](../conceptmap/README.md) for `translate` and [`pkg/fhirpath`](../fhirpath/README.md) for `evaluate`.

---

## How it fits in the ecosystem

```text
QuestionnaireResponse ──► pkg/sdc ($extract)
                                │
                                ▼
                         structuremap.Engine
                                │
              ┌─────────────────┼─────────────────┐
              ▼                 ▼                 ▼
        StoreResolver      FHIRPath          conceptmap.Translator
              │                                   │
              ▼                                   ▼
        StructureMap resource              target Codings
```

Runtime (`wire.go`) wires `structuremap.NewExtractor` with strict engine, store resolver, terminology-backed translator, and `StoreCardinalityResolver`.

Maps are FHIR **StructureMap** resources referenced by Questionnaire `sourceStructureMap` URL.

---

## When to use it

- **QR → clinical resources** (Patient, Observation, …) declaratively
- **Versioned extraction rules** as FHIR resources
- **CI golden tests** without external mapping servers

Use manual Go only for trivial demos. Do not treat this as general-purpose ETL outside SDC `$extract`.

---

## Usage modes

### 1. SDC / HTTP `$extract` (recommended)

POST `QuestionnaireResponse/$extract` — see [pkg/sdc/README.md](../sdc/README.md). Runtime delegates when `sourceStructureMap` is set. Apply resulting transaction bundle via `core.ProcessTransactionBundle`.

### 2. `NewExtractor` in custom runtime

```go
extractor := structuremap.NewExtractor(structuremap.Config{
    Resolver: &structuremap.StoreResolver{Resources: rs, Registry: defs},
    Engine: structuremap.Engine{
        FHIRPath: fp, Strict: true,
        Translator: conceptmap.Translator{Resolver: cmRes},
        Cardinality: &structuremap.StoreCardinalityResolver{Store: defs},
    },
})
```

### 3. Direct `Engine.Execute`

```go
out, err := engine.Execute(ctx, m, structuremap.ExecuteInput{"src": qrMap})
```

See `engine_test.go`: `TestEngineExecutesMinimalPatientExtraction`, `exampleExtractionMap`, `exampleResponse()`.

### 4. Static resolver (tests)

```go
m, err := structuremap.StaticResolver{canonical: mapDef}.Resolve(ctx, canonical)
```

### 5. Parse / validate maps

`ParseMap([]byte)`, `ResolveEnvelope(*types.ResourceEnvelope)` without executing.

---

## Examples (APIs from this repo)

**Entry group** (`TestEntryGroupSelectedByMapName`): `Map.Name == "PopulatePatient"` selects that group.

**Transforms:**

```go
Engine{}.applyTransform(ctx, "id", []Parameter{
    {ValueString: "http://example.org/mrn"}, {ValueString: "123"}, {ValueString: "MR"},
}, nil, nil)

engine := Engine{Translator: conceptmap.Translator{Resolver: conceptmap.StaticResolver{...}}}
engine.applyTransform(ctx, "translate", []Parameter{{ValueString: mapURL}}, nil, sourceCoding)
```

**ExtractorRun errors:** nil resolver; missing `sourceStructureMap` on Questionnaire.

**ExecuteInput from ExtractorRun:**

```go
ExecuteInput{
    "questionnaire": qMap, "questionnaireResponse": rMap,
    "src": rMap, "q": qMap,
}
```

**Nested items** — `TestNestedRepeatingPathNameGiven`: `linkId = 'name'` → `Patient.name.given`.

---

## Where it fits

```text
QuestionnaireResponse ──► pkg/sdc ──► structuremap ──► Bundle ──► pkg/core
                              │
                              ├── conceptmap (translate)
                              ├── fhirpath (evaluate)
                              └── store (StructureMap, StructureDefinition)
```

Examples: [modules/sdc/examples/](../../modules/sdc/examples/).

---

## Limits

- Subset of Mapping Language — unsupported transforms error at runtime
- `StoreResolver` rebuilds index each resolve (list up to 10k StructureMap ids)
- Strict mode in production — maps must match SDC paths
- No remote map server — resolver must load maps locally
- Output is JSON only — no automatic persist
- Extend engine via `transforms.go` + tests, not assumed HAPI parity

---

## Troubleshooting

| Symptom | Likely cause |
|---------|----------------|
| `questionnaire has no sourceStructureMap` | Questionnaire missing SDC extension URL |
| `StructureMap resolver is unavailable` | Nil `Config.Resolver` on extractor |
| Empty output | Entry group name does not match `StructureMap.name` |
| `translate` errors | ConceptMap missing from resolver — see [conceptmap](../conceptmap/README.md) |

```bash
go test ./pkg/structuremap/... -count=1
```

---

## Related docs

- [pkg/sdc/README.md](../sdc/README.md)
- [pkg/conceptmap/README.md](../conceptmap/README.md)
- [pkg/fhirpath/README.md](../fhirpath/README.md)
- [Composition patterns — SDC](../../docs/composition-patterns.md#questionnaire--sdc-workflow)
- [modules/sdc/examples/](../../modules/sdc/examples/)
- [HL7 StructureMap](https://hl7.org/fhir/structuremap.html)
