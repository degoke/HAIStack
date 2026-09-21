# Changelog: Parquet-on-FHIR & analytics export

## Unreleased

### Added

- Parquet-on-FHIR nested export via `_parquetLayout=fhir` on `$viewdefinition-run`, `$viewdefinition-export`, and analytics sinks.
- `ExportPayload.since` and `ExportPayload.parameters` for analytics background export jobs.
- `ResultMetadata.maxLastUpdated` for data-clock watermark advancement.
- HTTP `$viewdefinition-export` support for `_subject`, `_actor`, and Parameters body fields (plus `subject`, `actor`, and custom parameters in POST body).
- HTTP `$viewdefinition-run` support for the same operation context fields.
- `store.BlobStoreWithStream` and `binary.BlobStoreWithStream` (`PutStream`) so parquet lakehouse and `$viewdefinition-export` uploads stream from the temp file without `os.ReadFile`. Helpers: `store.PutBlob`, `store.PutBlobFromPath`, `binary.CopyChunks`, `binary.PutBlobStream`.
- `store.BlobStoreWithOpen` / `binary.BlobStoreWithOpen` (`Open`) so downloads stream without assembling a full `[]byte`. Helpers: `store.OpenBlob`, `binary.ChunkReader`. `export.Service.OpenFile` / `view.ExportService.OpenFile` stream HTTP artifact downloads.

### Changed

- **`analytics.ExportHandler` / `ExportHandlerWithConfig`** now require a third argument: `*WatermarkStore` (pass `nil` to disable watermark auto-fill and advance).
- Incremental watermarks prefer `maxLastUpdated` from exported resources over process wall clock when available.
- `Metadata.filtered` semantics are mode-specific (expanded view rows vs matching FHIR resources); see `pkg/view/README.md`.
- Lakehouse blob upload and `$viewdefinition-export` parquet artifacts stream from the temp file via `store.BlobStoreWithStream` / `view.ExportFileStoreWithStream` instead of `os.ReadFile` + `Put([]byte)`.
- Bulk `$export` writes NDJSON to a temp file and uploads with `FileStore.PutStream`. `PrefixedFileStore.Open` returns the backend stream and does not re-hydrate `Location` (pointer-only BYTEA rows are resolved in Postgres `BlobStore.Open`). S3 `PutStream` rejects a 2xx whose hashed byte count differs from the declared `Content-Length`.

### Deprecated

- `Executor.CollectMatchingResources` — use `WriteParquetFHIRExport` for large exports.

### Known limitations

- INT96 date annotation columns use INT64 TIMESTAMP(MILLIS); tracked in [issue #42](https://github.com/degoke/HAIStack/issues/42).
- Postgres `store.BlobStore` (`hai_binary_object.data` BYTEA) still materializes streaming uploads for INSERT; object-store adapters and chunk stores stream.
- HTTP `$viewdefinition-run` / `$viewdefinition-export` Parameters parsing accepts `valueString` only (see `pkg/view/README.md`).

### Review notes

- Functional changes for HTTP run/export parity and watermark tests are in commit `4301dac`; later commits on the same branch are CI housekeeping (gofmt, golangci-lint, `go mod tidy`) and lint-driven dead-code removal.
