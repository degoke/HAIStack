# External server adapter template

This track is **vendor-neutral**: workload YAML does not mention HAIStack.
To compare another FHIR implementation (HAPI FHIR, Firely, IBM, etc.):

1. Implement the same three operations against the server:
   - `read` — `GET {base}/{resourceType}/{id}`
   - `scan-read` — list ids (search `_elements=id` or a bulk dump) then read
   - `view` — if the server supports SQL-on-FHIR ViewDefinitions; otherwise skip and report `unsupported`
2. Use the **same** generated dataset (run `go run ./research/benchmarks -dump DIR` or copy the in-memory generator with `seed=11`).
3. Record per-workload p50/p95 latency and operations/second. Do not publish a single blended score.
4. Keep PHI out of dumps. Only synthetic resources from this repository are licensed Apache-2.0.

Suggested Go seam:

```go
type Adapter interface {
    Read(ctx context.Context, resourceType, id string) error
    ListIDs(ctx context.Context, resourceType string, limit int) ([]string, error)
    ExecuteView(ctx context.Context, name, version string) (rowCount int, err error)
}
```

HAIStack's runner in `runner.go` is the reference `Adapter` over
`research/internal/researchutil.MemoryResourceStore` and `pkg/view`.
