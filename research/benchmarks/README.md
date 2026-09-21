# Track A — Vendor-neutral FHIR benchmark suite

Portable workloads and seeded synthetic datasets for comparing FHIR
implementations on **workload-specific** measures, not a single score.

## Reproduce

```bash
make research-benchmarks
go test ./research/benchmarks -count=1
go run ./research/benchmarks/cmd
```

Default size is `small`. Medium and large are generated from the same seed:

```bash
go run ./research/benchmarks/cmd -size medium
```

## Workloads

Defined in [`testdata/workloads.yaml`](./testdata/workloads.yaml):

| Workload | Operation | What it measures |
|----------|-----------|------------------|
| `crud-read` | Patient reads by id | Envelope read latency / correctness |
| `view-observation` | SQL-on-FHIR ViewDefinition | Projection completeness |
| `ai-run-view` | `pkg/ai` `run_view` | Policy + view + audit path (HAIStack-only) |

Methodology: report per-workload operation counts, row counts, and elapsed
time. Do **not** collapse results into one ranking. Implementations that skip
AI tools should omit `ai-run-view` rather than score zero.

## Dataset generator

`Generate(seed, size)` produces deterministic Patients and Observations.
Sizes:

| Size | Patients | Observations |
|------|----------|----------------|
| small | 10 | 20 |
| medium | 50 | 100 |
| large | 200 | 400 |

## External adapters

[`adapter.go`](./adapter.go) defines `Adapter`. [`adapter_hapi.go`](./adapter_hapi.go)
is a documented template for an HTTP HAPI client. It is not executed in CI.
