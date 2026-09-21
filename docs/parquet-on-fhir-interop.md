# Parquet-on-FHIR interop profile

HAIStack implements [Parquet-on-FHIR](https://github.com/aehrc/parquet-on-fhir) nested resource export via `_parquetLayout=fhir`.

## Compatibility matrix

| Topic | Parquet-on-FHIR spec | HAIStack export | Query engine notes |
|-------|----------------------|-----------------|-------------------|
| Nested LIST/GROUP layout | `field.list.element.*` | Same | DuckDB, Spark, Trino read LIST columns |
| Choice types | `valueInteger`, `valueQuantity`, … | Same | Derived from StructureDefinition + observed data |
| Primitive wrappers | `_field` groups | Same | Includes nested extension annotations when typed |
| Date range annotations | `int96` + `TIMESTAMP(MILLIS)` | Default `int64` + `TIMESTAMP(MILLIS)`; optional `int96` + `TIMESTAMP(MILLIS)` via `_parquetTimestampEncoding=int96` | INT64 remains default for parquet-go map writer compatibility and modern engines (DuckDB, Spark, Trino). INT96 uses a typed `parquet.Row` writer. Values are Unix millis packed with `Int64ToInt96`, matching `TIMESTAMP(MILLIS)` — not Hive/Impala julian-day + nanos-of-day. Engines that ignore the logical type and treat every INT96 as a Hive timestamp will misread values. parquet-go `OpenFile` remaps `TIMESTAMP` to INT64, so `Schema()`/`Pages()` cannot read these columns; use `ReadInt96MillisColumn`. Millisecond UTC ranges are equivalent for filtering. |
| Decimal annotations | `fixed_len_byte_array(16)` DECIMAL(38,6) | Same | Compatible with Spark/DuckDB decimal reads |
| Quantity canonical | UCUM canonical group | Temperature, length, mass UCUM codes | Other UCUM codes omit canonical group |
| String primitives | Spec table lists `binary` + STRING | `parquet.String()` (BYTE_ARRAY + STRING) | Equivalent in modern parquet readers |
| Lossless round-trip | Required by spec | Best-effort | Invalid optional annotations are skipped; floats formatted with 6 decimal places |

## Verified readers

Automated tests validate schema shape against spec Patient/Observation examples in `pkg/parquetfhir/writer_test.go` (`TestInteropSpec*`).

Manual verification recommended for your target stack:

- **DuckDB** — `SELECT * FROM read_parquet('file.parquet')`
- **Apache Spark 3.x** — `spark.read.parquet(path)`. Spark/Hive often treat INT96 as julian-day + nanos-of-day and will misread HAIStack millis-packed values unless they honor `TIMESTAMP(MILLIS)`.
- **Trino/Presto** — hive parquet connector

## API entry points

- `$viewdefinition-run?_format=parquet&_parquetLayout=fhir`
- `$viewdefinition-export` with the same parameters
- Optional `_parquetTimestampEncoding=int96` on query `_parquetTimestampEncoding` / `parquetTimestampEncoding`, or Parameters body `parquetTimestampEncoding` / `_parquetTimestampEncoding` (default `int64`). A non-empty body value overrides query, matching export body `format`. `_parquetLayout` and run `_format` remain query-only.
- Analytics sinks: `ParquetLayout: view.ParquetLayoutFHIR` + `Executor` with `ProfileCatalog`; `TimestampEncoding: view.TimestampEncodingInt96` for spec INT96 columns

## Incremental export

View export and analytics FHIR parquet paths honor `ExecuteRequest.Since`. Analytics runner populates `Result.ExecRequest` when skipping flat view execution so lakehouse/manifest sinks apply watermark filters. Background export jobs (`ExportPayload.since`) auto-fill from `WatermarkStore` when configured.

Watermarks are stored at the exported `maxLastUpdated` (inclusive). Search prefilters apply `_lastUpdated=gt{watermark}`; envelope re-checks use strict `Before(since)`.

## Memory behavior

`WriteResourcesStreaming` performs one candidate scan, observes schema from each resource, spills raw JSON to a temp NDJSON file, then encodes parquet in bounded row groups. Lakehouse blob uploads write parquet to a temp file before `BlobStore.Put` to avoid duplicating an in-memory buffer during encoding.

### Sizing guidance

| Stage | Peak memory driver |
|-------|-------------------|
| Resource scan + spill | One resource JSON + NDJSON encoder buffer |
| Parquet encode | One row group of prepared rows (default 1000) |
| Blob / export artifact upload | Full compressed parquet file loaded for `Put` |

For moderate exports (tens of MB parquet), in-process buffering is fine. Multi-GB lakehouse loads should use filesystem partitions (`LakehouseConfig.RootDir`) or a future streaming blob upload API. `CollectMatchingResources` is deprecated for large exports because it retains every matching resource in RAM.
