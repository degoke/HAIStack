# haistack-parquetfhir (`pkg/parquetfhir`)

[Parquet-on-FHIR](https://github.com/aehrc/parquet-on-fhir) nested resource layouts for analytics export and lakehouse ingestion.

## What it does

`pkg/parquetfhir` is the **encoder** that turns FHIR resource JSON into spec-oriented **nested Apache Parquet** files. It does not run ViewDefinitions, schedule jobs, or serve HTTP—that work lives in [`pkg/view`](../view/README.md) and [`pkg/analytics`](../analytics/README.md), which call into this package when FHIR layout is selected.

Core behavior:

- **Schema from StructureDefinitions** — Starts from a base resource `StructureDefinition` (for example bundled R4 `Patient` or `Observation`), then **prunes** to the union of fields observed in exported payloads so unused SD paths do not create empty columns.
- **Profile merge** — URLs in `meta.profile` resolve through a `validate.ProfileCatalog`; matching profiles **merge** additional elements into the schema index (choice types, extensions, constraints on nested paths).
- **Spec annotations** — Date and `dateTime` values emit companion `__field_start` / `__field_end` columns with `TIMESTAMP(MILLIS)` logical type. Decimals use `__field_numeric` with `DECIMAL(38,6)` on fixed-length byte arrays. `Quantity` values with UCUM codes in temperature, length, or mass groups get `__valueQuantity_canonical` structures.
- **Contained resources** — Inline `contained` entries merge into the contained LIST schema instead of opaque JSON blobs.
- **Primitive wrappers and extensions** — `_field` groups, nested extensions, and choice-typed fields (`valueInteger`, `valueQuantity`, …) follow LIST/GROUP nesting from the SD index.
- **Streaming write path** — `WriteResourcesStreaming` uses a **two-pass replay** model: one scan observes schema and spills NDJSON to disk, then encodes Parquet in bounded row groups (default 1000 rows) without holding the full export in memory.

Timestamp annotation columns default to **INT64** + `TIMESTAMP(MILLIS)` for compatibility with parquet-go map writers and modern engines. Optional **INT96** physical type matches the Parquet-on-FHIR spec when `WithTimestampEncoding(TimestampEncodingInt96)` or `_parquetTimestampEncoding=int96` is set.

## How it fits in the ecosystem

HAIStack separates **what to export** (ViewDefinition execution, search filters, watermarks) from **how bytes are laid out on disk** (flat view columns vs nested FHIR Parquet).

| Layer | Package | Role |
|-------|---------|------|
| Definitions | `registry`, `validate` | StructureDefinitions and profile catalogs |
| Execution | `view` | Row generation, `$viewdefinition-run` / `$viewdefinition-export` |
| Scheduling / sinks | `analytics` | Lakehouse, warehouse, manifest parquet targets |
| **Layout** | **`parquetfhir`** | Nested Parquet-on-FHIR encoding |

Flat parquet (`_parquetLayout=flat`) stays in `pkg/view` and does not import this package for column shaping. FHIR layout delegates schema build and row encoding here via `view/parquet_fhir_export.go`.

Downstream consumers (DuckDB, Spark, Trino, AEHR tooling) read the same nested column names the spec describes; see the compatibility matrix in [docs/parquet-on-fhir-interop.md](../../docs/parquet-on-fhir-interop.md).

## When to use it

Use Parquet-on-FHIR layout when you need:

- **Interop** with tools and documentation built around the AEHR/C Parquet-on-FHIR nested model.
- **Analytics on full resource shape** — extensions, contained resources, quantities, and choice types as typed columns rather than serialized JSON strings.
- **Large batch exports** where streaming and row-group batching matter (`WriteResourcesStreaming`).

Prefer **flat** view Parquet when you only need ViewDefinition projection columns for SQL reporting and do not require nested FHIR fidelity.

You typically **do not** import `parquetfhir` directly in application handlers; set `_parquetLayout=fhir` on view operations or configure analytics sinks unless you are building custom ETL.

## Usage modes

### 1. View `$viewdefinition-run` (query)

Request parquet output with FHIR nested layout:

```http
GET .../$viewdefinition-run?_format=parquet&_parquetLayout=fhir
```

Optional timestamp physical type:

```http
GET ...?_format=parquet&_parquetLayout=fhir&_parquetTimestampEncoding=int96
```

Wiring: [`pkg/http/view_operations.go`](../http/view_operations.go) passes layout and encoding into the view executor; encoding is parsed via `parquetfhir.ParseTimestampEncoding`.

### 2. View `$viewdefinition-export` (async artifact)

Same query parameters as run; export jobs record `parquetLayout` and `timestampEncoding` in job metadata when format is parquet. See [pkg/view/README.md](../view/README.md) (Parquet export sizing).

### 3. Analytics lakehouse / manifest sinks

Configure sinks with `ParquetLayout: view.ParquetLayoutFHIR`, an `Executor` that supplies `ProfileCatalog`, and optional `TimestampEncoding: view.TimestampEncodingInt96`. Documented in [pkg/analytics/README.md](../analytics/README.md) and [docs/parquet-on-fhir-interop.md](../../docs/parquet-on-fhir-interop.md).

### 4. Batch encode in-process (`WriteResources`)

Small or test-sized batches can write directly to an `io.Writer`:

```go
err := parquetfhir.WriteResources(w, sd, catalog, []map[string]any{resource}, opts...)
```

### 5. Streaming encode (`WriteResourcesStreaming`)

Production-sized exports should use the streaming API (also used by view FHIR export):

```go
n, err := parquetfhir.WriteResourcesStreaming(ctx, w, sd, catalog, func(yield func(map[string]any) error) error {
    for _, res := range scan() {
        if err := yield(res); err != nil {
            return err
        }
    }
    return nil
}, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
```

### 6. Read INT96 annotation columns (downstream tools)

When files use INT96 timestamps, parquet-go `OpenFile` cannot decode those columns through the normal API. Use:

```go
times, err := parquetfhir.ReadInt96MillisColumn(r, size, "effectivePeriod", "__start_start")
```

Document this for any reader you ship alongside exports.

## Examples (from tests and public API)

**Resolve base StructureDefinition** (view export uses the same helper):

```go
sd, err := parquetfhir.ResolveStructureDefinition(catalog, "Patient")
```

**Patient birthDate schema** (`TestInteropSpecPatientExampleSchema` in `writer_test.go`):

```go
resources := []map[string]any{{
    "resourceType": "Patient",
    "id":           "example",
    "birthDate":    "1970-01-01",
}}
data := writeParquet(t, sd, resources)
// Expect columns: resourceType, id, birthDate, __birthDate_start, __birthDate_end
```

**Observation with effectivePeriod** (`TestInteropSpecObservationPeriodStartINT96TimestampAnnotations`):

```go
resources := []map[string]any{{
    "resourceType": "Observation",
    "id":           "obs-period",
    "status":       "final",
    "effectivePeriod": map[string]any{
        "start": "2022-02-10T00:00:00Z",
        "end":   "2022-02-11T00:00:00Z",
    },
}}
data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
```

**Parse timestamp encoding** (`TestParseTimestampEncoding`):

```go
enc, err := parquetfhir.ParseTimestampEncoding("int96") // or "", "int64"
norm := parquetfhir.NormalizeTimestampEncoding(enc)
```

**Schema builder only** (advanced tests in `index_test.go`):

```go
builder, err := parquetfhir.NewSchemaBuilder(sd, catalog)
builder.ObserveResource(raw)
schema := builder.BuildSchemaWith(parquetfhir.TimestampEncodingInt64)
```

Golden tests also cover contained Organization on Patient, quantity canonical kelvin, extension decimals, partial effective dateTime, and streaming single-pass collections (`TestWriteResourcesStreamingSinglePassCollection`).

## Configuration / key types

| Type / constant | Purpose |
|-----------------|--------|
| `WriteOption` | Functional options for `WriteResources*` |
| `WithTimestampEncoding(TimestampEncoding)` | Select INT64 (default) or INT96 annotation columns |
| `TimestampEncoding` | `"int64"` or `"int96"` |
| `TimestampEncodingInt64`, `TimestampEncodingInt96` | Constants |
| `ParseTimestampEncoding`, `NormalizeTimestampEncoding` | HTTP/query/body parsing |
| `DefaultRowGroupSize` | `1000` — buffered row groups during encode |
| `NewSchemaBuilder` | Observe resources and build pruned schema |
| `SchemaBuilder.BuildSchema`, `BuildSchemaWith` | Produce `*parquet.Schema` |
| `ResolveStructureDefinition` | Base SD from catalog by resource type |
| `WriteResources`, `WriteResourcesStreaming` | Encode APIs |
| `ReadInt96MillisColumn`, `ResolveInt96Column` | INT96 read workaround |

HTTP parameters (handled in `pkg/view`, not duplicated here):

- `_parquetLayout=fhir`
- `_parquetTimestampEncoding=int96` (query or Parameters body `parquetTimestampEncoding`)

## Where it fits

```text
FHIR store / search
        │
        ▼
pkg/view.Execute / export scan
        │
        ├── _parquetLayout=flat  ──► flat Parquet (view)
        │
        └── _parquetLayout=fhir  ──► pkg/parquetfhir.WriteResourcesStreaming
                                              │
                                              ▼
                                    nested .parquet (lakehouse / artifact)
```

Analytics runner may skip flat execution but still pass resources and `ProfileCatalog` into the same streaming encoder for lakehouse partitions.

## Limits

- **Schema breadth** depends on observed payloads and available profiles; resources never seen in the export pass produce no columns even if the SD defines them.
- **UCUM canonical groups** are implemented for temperature, length, and mass; other units omit the canonical group.
- **INT96** values are Unix millis packed with `Int64ToInt96`, not Hive julian-day INT96; engines that ignore `TIMESTAMP(MILLIS)` may misread INT96 columns.
- **parquet-go read path** remaps TIMESTAMP to INT64; INT96 files require `ReadInt96MillisColumn` for annotation columns.
- **Lossless round-trip** is best-effort: invalid optional annotations may be skipped; floats are formatted with six decimal places.
- **Spec coverage** is validated by package tests (`TestInteropSpec*`, quantity, contained, extensions)—not every edge case in the full Parquet-on-FHIR document may be implemented; check tests before claiming complete spec compliance.

## Related docs

- [Parquet-on-FHIR interop](../../docs/parquet-on-fhir-interop.md)
- [SQL-on-FHIR gap — Parquet section](../../docs/sql-on-fhir-gap.md)
- [pkg/view/README.md](../view/README.md)
- [pkg/analytics/README.md](../analytics/README.md)
- [pkg/analytics/EDGE.md](../analytics/EDGE.md)
- [doc.go](./doc.go) — package comment and parameter summary
