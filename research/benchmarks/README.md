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
CI). Override with `HAISTACK_BENCH_SIZE=medium|large` or `-size`.
Write the same synthetic JSON for an external adapter with
`go run ./research/benchmarks -dump DIR`.

## Contents

| Path | Role |
|------|------|
| `workloads/*.yaml` | Portable workload definitions (`read`, `scan-read`, `view`) |
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

`count` is the operation budget in the YAML, not a fraction of the
generated store. `-size` / `HAISTACK_BENCH_SIZE` only changes how many
Patients and Observations are seeded (20/40, 100/200, 400/800). The
bundled counts stay 40 reads, 40 scan-list ids, and 10 view executions
at every size — a large dump still lists at most 40 of 800 Observations.

| `type` | What `count` means |
|--------|--------------------|
| `read` | Number of `Read` calls (round-robin over the seeded pool) |
| `scan-read` | `ListIDs` page size, then one `Read` per returned id |
| `view` | Number of `Execute` calls; page size is `limit` (required) |

The HAIStack reference runner implements `scan-read` as
`MemoryResourceStore.ListIDs` followed by `Read`. That is not FHIR
`_search`. External adapters may list ids with search (`_elements=id`)
or a bulk dump; say which path you used when comparing.

For `scan-read`, p50/p95 are computed over **one combined sample pool**:
the list call plus each subsequent read. They are not separate list vs
read percentiles. `view` uses the YAML `limit` as the row page size
(100 in `view-execute.yaml`).

## Citation

Cite this directory via the repository [`CITATION.cff`](../../CITATION.cff).
A Zenodo deposit can attach the generated small/medium/large dumps when
published.
