// Package search implements haistack-search: registry-driven FHIR search indexing,
// query parsing/planning, Postgres execution, and bundle-ready search results.
//
// # Architecture
//
// haistack-search is the FHIR search library for Health AI Stack. It keeps reusable
// lower-level components in this package while exposing search.Service as the main
// entrypoint for advanced search execution.
//
//   - Registry / SnapshotRegistry — resolve enabled SearchParameters from haistack-registry
//   - RegistryIndexer — search.Indexer implementation for pkg/core write-path indexing
//   - ParseQuery / ResolveQuery / BuildPlan — parse and plan FHIR query params
//   - StoreExecutor — execute typed index lookups via store.SearchQueryExecutor
//   - Service — high-level search execution returning bundle-ready Result values
//   - ReindexWorker / ReindexJobRunner / ReindexNotifier — rebuild index rows and enqueue on registry changes
//
// # Advanced search features
//
// Postgres-first execution supports:
//
//   - _include and _revinclude (direct, non-wildcard)
//   - single-hop chained search (e.g. subject.name)
//   - composite SearchParameters from the registry
//   - modifiers (:exact, :contains on string; date/number prefixes)
//   - _sort on registry-backed parameters plus _id / _lastUpdated
//   - _count / _offset paging with deterministic ordering
//   - _summary and _elements response projection
//   - Postgres full-text search via indexed text documents
//
// Unsupported semantics are rejected explicitly (ErrUnsupportedFeature, ErrInvalidQuery).
//
// # Indexing
//
// RegistryIndexer evaluates each installed SearchParameter expression with pkg/fhirpath,
// normalizes extracted values into typed field keys (token.*, string.*, date.*,
// reference.*, composite.*, text.*), and emits store.SearchIndexEntry rows consumed
// by store.SearchStore.
//
// # Query semantics
//
//   - Repeated parameters AND together
//   - Comma-separated values OR within one parameter occurrence
//   - _count and _offset provide offset paging; _sort supports registry-backed fields
//
// # Backends
//
// Postgres is the first complete execution backend via store.SearchAdvancedExecutor.
// SQLite supports typed index persistence and basic LookupMatch for tests and embedded
// nodes, but not the full advanced query feature set.
//
// # Integration
//
// Wire RegistryIndexer into pkg/core for write-path indexing:
//
//	indexer, _ := search.NewRegistryIndexer(search.RegistryIndexerConfig{
//	    Registry: search.NewSnapshotRegistry(snapshot),
//	    Engine:   fhirpathEngine,
//	})
//
// Execute search through Service:
//
//	svc, _ := search.NewService(search.ServiceConfig{
//	    Registry:  search.NewSnapshotRegistry(snapshot),
//	    Executor:  search.NewStoreExecutor(db.SearchStore(), db.ResourceStore()),
//	    Resources: db.ResourceStore(),
//	})
//	result, err := svc.Search(ctx, "Patient", queryParams)
package search
