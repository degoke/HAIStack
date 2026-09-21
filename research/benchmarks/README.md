# Track A — Vendor-neutral FHIR benchmark suite

Reference workloads and a HAIStack runner for **workload-specific**
evaluation (latency and throughput per operation), not a single score.

## Reproduce

```bash
make research-benchmarks
# or
go test ./research/benchmarks
go run ./research/benchmarks
```

By default the runner uses the **small** seed dataset (fast enough for
CI). Override with `HAISTACK_BENCH_SIZE=medium|large`.

## Contents

| Path | Role |
|------|------|
| `workloads/*.yaml` | Portable workload definitions (read, search-scan, view) |
| `generate.go` | Seeded synthetic dataset (`seed=11`) |
| `runner.go` | HAIStack in-memory runner (`researchutil.MemoryResourceStore` + `pkg/view`) |
| `adapters/README.md` | Template for wiring HAPI or another server |

## Sizes

| Name | Patients | Observations |
|------|----------|----------------|
| small | 20 | 40 |
| medium | 100 | 200 |
| large | 400 | 800 |

Datasets are generated from a fixed seed; they are not stored as PHI and
are not sampled from real EHR extracts.

## Methodology

Report **per-workload** measurements (p50/p95 latency, operations/sec).
Do not collapse heterogeneous operations into one leaderboard number.
Compare implementations only on the same workload YAML and size.

## Citation

Cite this directory via the repository [`CITATION.cff`](../../CITATION.cff).
A Zenodo deposit can attach the generated small/medium/large dumps when
published.
