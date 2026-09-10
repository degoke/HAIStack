# Verifying third-party FHIR Bulk Data export

Use this checklist before building downstream Spark, dbt, or ML jobs against a cloud provider or vendor "FHIR export" API.

## Pre-flight

1. Confirm the server advertises Bulk Data export in its CapabilityStatement (optional but helpful).
2. Obtain a backend-service token with export permission (HAIStack: `bulk-export` permission + `CanBulkExport` policy).
3. Record the FHIR base URL and whether exports require same-origin status polling.

## Kickoff (`GET [base]/$export` or `GET [base]/Group/{id}/$export`)

| Check | Expected |
|-------|----------|
| HTTP status | `202 Accepted` |
| `Prefer: respond-async` | Required on kickoff |
| `Content-Location` | Present; absolute or same-origin relative URL |
| Query params | `_type`, `_since`, `_typeFilter`, `_outputFormat` honored or rejected explicitly |

## Status polling (`GET Content-Location`)

| Check | Expected |
|-------|----------|
| In progress | `202 Accepted`, optional `X-Progress` header |
| Complete | `200 OK` with manifest JSON body |
| Unauthorized | `401`/`403` without valid token |

## Manifest JSON structure

Required fields:

```json
{
  "transactionTime": "2024-01-01T00:00:00Z",
  "request": "GET /fhir/$export?...",
  "requiresAccessToken": true,
  "output": [
    { "type": "Patient", "url": "https://example/fhir/$export/files/job/Patient.ndjson" }
  ]
}
```

Optional:

- `error[]` — same shape as `output[]` for OperationOutcome NDJSON files

## NDJSON artifacts

| Check | Expected |
|-------|----------|
| Content-Type | `application/fhir+ndjson` or `application/ndjson` |
| Line format | One FHIR resource JSON object per line |
| Type consistency | Each file contains resources matching `output[].type` |
| Delete-after-download | Files removed after TTL (vendor-specific; HAIStack: operator-managed file store) |

## Group export

- `GET /Group/{id}/$export` returns `202` with status URL.
- Exported Patient resources match group membership (vendor-specific filtering rules apply).

## Self-test with HAIStack

Run the in-repo round-trip tests:

```bash
go test ./pkg/export/... ./pkg/client/... -run BulkExport -count=1
go test ./pkg/runtime/... -run BulkExport -count=1
```

Optional external compatibility (requires a running reference server):

```bash
TEST_FHIR_SERVER_URL=https://hapi.example/fhir go test -tags bulk_compat ./pkg/client/... -run BulkCompat -count=1
```

## Red flags (do not build pipelines yet)

- Kickoff returns `200` with a Bundle instead of async `202`
- Status URL is cross-origin without documented CORS/token rules
- Manifest URLs expire before download completes with no retry guidance
- NDJSON lines are not valid JSON or mix resource types within a file
- Export ignores authorization entirely

## References

- HL7 Bulk Data IG: https://build.fhir.org/ig/HL7/bulk-data/
- HAIStack client: `pkg/client/bulk.go`
- HAIStack server: `pkg/export`, `pkg/http/bulk.go`
