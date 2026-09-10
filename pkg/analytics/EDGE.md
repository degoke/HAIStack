# Edge and Postgres analytics operations

HAIStack edge mode co-locates OLTP (interactive REST) and analytics (view refresh, reporting tables) in one Postgres instance. Use the guidance below to keep clinical traffic responsive.

## When to use which path

See the root [README transactional vs analytical decision tree](../../README.md#transactional-vs-analytical-data-paths).

| Workload | Mechanism |
|----------|-----------|
| Interactive CRUD/search | FHIR REST (`pkg/http`) |
| Large cohort NDJSON export | Bulk Data `$export` (`pkg/export`) |
| Dashboards / flat columns | View refresh → reporting tables (`pkg/analytics`) |
| Incremental reporting | CDC-triggered `analytics.refresh` jobs + cursor-based refresh |

## Connection and pool limits

- **OLTP (primary DSN):** Keep the primary pool sized for interactive REST and write sessions. Default Postgres pool max is 10 connections (`postgres.WithMaxConns`).
- **Analytics reads:** When a read replica is available, configure `runtime.WithPostgresReadReplica(dsn)` so view execution scans the replica via `TenantDB.ReadOnlyResourceStore()`.
- **Reporting writes:** Refresh metadata and reporting table rows always write to the primary (`ReportingTableStore` on the primary pool).

## Scheduling

- Run full or incremental view refreshes **off peak** on edge nodes sharing one database.
- CDC (`analytics.CDCProcessor`) enqueues refresh jobs after outbox events; combine with `runtime.WithAnalyticsConcurrency(1)` (default) so at most one refresh job runs concurrently.

## SQLite edge devices

SQLite mode does not provide Postgres reporting tables. On devices:

- Use Bulk Data export or sync push for upstream aggregation.
- Do not schedule `analytics.refresh` jobs locally.

## Protecting OLTP

1. Enable HTTP rate limiting: `runtime.WithHTTPRateLimit`.
2. Limit analytics job concurrency: `runtime.WithAnalyticsConcurrency(1)`.
3. Do not paginate `_search` for large exports — use `$export`.
4. Prefer read replica DSN for view scans when replicas exist.

## Cloud mode

Register `runtime.WithExternalWarehouse` to route reporting tables to an external store, or use the built-in `PostgresReportingWarehouse` adapter for edge-all-in-one Postgres.
