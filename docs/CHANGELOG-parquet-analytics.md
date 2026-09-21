# Changelog: Parquet-on-FHIR & analytics export

## Unreleased

### Added

- Parquet-on-FHIR nested export via `_parquetLayout=fhir` on `$viewdefinition-run`, `$viewdefinition-export`, and analytics sinks.
- Optional Parquet-on-FHIR INT96 date annotations via `_parquetTimestampEncoding=int96` (typed row writer). INT64 TIMESTAMP(MILLIS) remains the default. INT96 values are Unix millis packed with `Int64ToInt96` (not Hive julian-day). parquet-go cannot natively read INT96+TIMESTAMP pages; `ReadInt96MillisColumn` round-trips them row-aligned (nil for nulls, full path when leaf names collide). Nested `Period.start`/`end` annotations (for example Observation `effectivePeriod`) are covered. HTTP run/export accept the encoding from query or Parameters body.
- `ExportPayload.since` and `ExportPayload.parameters` for analytics background export jobs.
- `ResultMetadata.maxLastUpdated` for data-clock watermark advancement.
- HTTP `$viewdefinition-export` support for `_subject`, `_actor`, and Parameters body fields (plus `subject`, `actor`, and custom parameters in POST body).
- HTTP `$viewdefinition-run` support for the same operation context fields.

### Changed

- **`analytics.ExportHandler` / `ExportHandlerWithConfig`** now require a third argument: `*WatermarkStore` (pass `nil` to disable watermark auto-fill and advance).
- Incremental watermarks prefer `maxLastUpdated` from exported resources over process wall clock when available.
- `Metadata.filtered` semantics are mode-specific (expanded view rows vs matching FHIR resources); see `pkg/view/README.md`.

### Deprecated

- `Executor.CollectMatchingResources` — use `WriteParquetFHIRExport` for large exports.

### Known limitations

- Blob lakehouse and async export artifacts buffer the full parquet file at upload time (`BlobStore` API).
- HTTP `$viewdefinition-run` / `$viewdefinition-export` Parameters parsing accepts `valueString` only (see `pkg/view/README.md`).

### Review notes

- Functional changes for HTTP run/export parity and watermark tests are in commit `4301dac`; later commits on the same branch are CI housekeeping (gofmt, golangci-lint, `go mod tidy`) and lint-driven dead-code removal.
