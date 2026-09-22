# External server adapter template

This track is **vendor-neutral**: workload YAML does not mention HAIStack.
To compare another FHIR implementation (HAPI FHIR, Firely, IBM, etc.):

1. Implement the same three operations against the server:
   - `read` — `GET {base}/{resourceType}/{id}`
   - `scan-read` — list ids then read. The HAIStack runner uses in-memory
     `ListIDs` + `Read` (not FHIR `_search`). External servers may list via
     search `_elements=id` or a bulk dump; report which.
   - `view` — execute `observation_view` with the YAML `limit` (page size)
     if the server supports SQL-on-FHIR ViewDefinitions; otherwise skip
     and report `unsupported`. `count` is executions, not rows.
2. Use the **same** generated dataset (run `go run ./research/benchmarks -dump DIR` or copy the in-memory generator with `seed=11`). `-size` changes dump cardinality only; honor the YAML `count` / `limit` even when the store is larger.
3. Record per-workload p50/p95 latency and operations/second. For
   `scan-read`, say whether percentiles mix list+read samples (the
   reference runner does) or split them. Do not publish a single blended score.
4. Keep PHI out of dumps. Only synthetic resources from this repository are licensed Apache-2.0.

Suggested Go seam for an **external** port (this repository does not
define or implement `Adapter`; `runner.go` is an inline `switch` over
`MemoryResourceStore` + `pkg/view`):

```go
type Adapter interface {
    Read(ctx context.Context, resourceType, id string) error
    ListIDs(ctx context.Context, resourceType string, limit, offset int) ([]string, error)
    ExecuteView(ctx context.Context, name string, limit int) (rowCount int, err error)
}
```

The reference runner calls `ListIDs(ctx, resourceType, count, 0)` then
`Read` for each id, and `ExecuteView` with the YAML `limit`.
