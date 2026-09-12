# Parquet-on-FHIR interop profile

HAIStack implements [Parquet-on-FHIR](https://github.com/aehrc/parquet-on-fhir) nested resource export via `_parquetLayout=fhir`.

## Compatibility matrix

| Topic | Parquet-on-FHIR spec | HAIStack export | Query engine notes |
|-------|----------------------|-----------------|-------------------|
| Nested LIST/GROUP layout | `field.list.element.*` | Same | DuckDB, Spark, Trino read LIST columns |
| Choice types | `valueInteger`, `valueQuantity`, … | Same | Derived from StructureDefinition + observed data |
| Primitive wrappers | `_field` groups | Same | Includes nested extension annotations when typed |
| Date range annotations | `int96` + `TIMESTAMP(MILLIS)` | `int64` + `TIMESTAMP(MILLIS)` | **Deviation:** tracked in [issue #42](https://github.com/degoke/HAIStack/issues/42); parquet-go map writers cannot encode deprecated INT96 arrays; millisecond UTC ranges are equivalent for filtering |
| Decimal annotations | `fixed_len_byte_array(16)` DECIMAL(38,6) | Same | Compatible with Spark/DuckDB decimal reads |
| Quantity canonical | UCUM canonical group | Temperature, length, mass UCUM codes | Other UCUM codes omit canonical group |
| String primitives | Spec table lists `binary` + STRING | `parquet.String()` (BYTE_ARRAY + STRING) | Equivalent in modern parquet readers |
| Lossless round-trip | Required by spec | Best-effort | Invalid optional annotations are skipped; floats formatted with 6 decimal places |

## Verified readers

Automated tests validate schema shape against spec Patient/Observation examples in `pkg/parquetfhir/writer_test.go` (`TestInteropSpec*`).

Manual verification recommended for your target stack:

- **DuckDB** — `SELECT * FROM read_parquet('file.parquet')`
- **Apache Spark 3.x** — `spark.read.parquet(path)`
- **Trino/Presto** — hive parquet connector

## API entry points

- `$viewdefinition-run?_format=parquet&_parquetLayout=fhir`
- `$viewdefinition-export` with the same parameters
- Analytics sinks: `ParquetLayout: view.ParquetLayoutFHIR` + `Executor` with `ProfileCatalog`

## Incremental export

View export and analytics FHIR parquet paths honor `ExecuteRequest.Since`. Analytics runner populates `Result.ExecRequest` when skipping flat view execution so lakehouse/manifest sinks apply watermark filters. Background export jobs (`ExportPayload.since`) auto-fill from `WatermarkStore` when configured.

## Memory behavior

`WriteResourcesStreaming` performs one candidate scan, observes schema from each resource, spills raw JSON to a temp NDJSON file, then encodes parquet in bounded row groups. Lakehouse blob uploads write parquet to a temp file before `BlobStore.Put` to avoid duplicating an in-memory buffer during encoding.
