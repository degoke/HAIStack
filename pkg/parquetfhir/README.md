# haistack-parquetfhir (`pkg/parquetfhir`)

[Parquet-on-FHIR](https://github.com/aehrc/parquet-on-fhir) nested resource layouts for analytics export.

## What it does

`pkg/parquetfhir` builds **Parquet schemas** from FHIR StructureDefinitions, prunes to fields present in exported payloads, and encodes resources with spec-oriented annotations (dates, decimals, Quantity canonical groups, contained resources, profile merges).

Features include:

- Streaming writer with a **two-pass replay** model for bounded memory  
- Optional `_parquetLayout=fhir` on view export/run and analytics lakehouse sinks  
- Timestamp encoding options (`TIMESTAMP(MILLIS)` on INT64 default; INT96 path for spec-aligned round-trip helpers)  

It does **not** replace [`pkg/view`](../view/README.md) execution or [`pkg/analytics`](../analytics/README.md) scheduling—it supplies the **encoder** used when Parquet layout is requested.

## When to use it

- **Lakehouse ingestion** — FHIR-shaped nested columns instead of ad-hoc JSON strings  
- **Interop with Parquet-on-FHIR tooling** — align with AEHR/C spec layouts  
- **Large batch exports** — streaming write path for many resources  

For simple flat CSV reporting tables, `analytics` default relational targets may suffice without nested Parquet.

## Usage modes

### 1. View export with layout parameter

Run a ViewDefinition export operation with `_parquetLayout=fhir` (query or Parameters) so the HTTP/view pipeline selects the Parquet-on-FHIR encoder. See [pkg/view/README.md](../view/README.md) — Parquet export sizing section.

### 2. Analytics sink

Configure an analytics lakehouse sink that delegates encoding to `parquetfhir` when writing Parquet artifacts. See [pkg/analytics/README.md](../analytics/README.md) and [docs/parquet-on-fhir-interop.md](../../docs/parquet-on-fhir-interop.md).

### 3. Direct encoder (advanced)

Import `parquetfhir` in custom ETL that already has StructureDefinitions and resource JSON batches—follow patterns in `pkg/parquetfhir` tests for schema build + `WriteResourcesStreaming`.

## Where it fits

```text
FHIR resources ──► pkg/view (rows/context)
                        │
                        └──► pkg/analytics (sink)
                                  │
                                  └──► pkg/parquetfhir (nested Parquet files)
```

## Limits

- Schema build depends on available StructureDefinitions and observed payload shapes—missing profiles may yield narrower columns until seen.  
- INT96 timestamp columns require `ReadInt96MillisColumn` on read due to parquet-go quirks; document this for downstream consumers.  
- Not every FHIR datatype edge case in the full Parquet-on-FHIR spec may be exercised—check golden tests in this package before claiming full spec coverage.

## Related docs

- [Parquet-on-FHIR interop](../../docs/parquet-on-fhir-interop.md)  
- [pkg/analytics/EDGE.md](../analytics/EDGE.md)
