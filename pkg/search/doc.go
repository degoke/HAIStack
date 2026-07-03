// Package search implements haistack-search: registry-driven FHIR search indexing,
// query parsing/planning, Postgres execution, and bundle-ready search results.
//
// # Architecture
//
// haistack-search is the FHIR search library for Health AI Stack. It keeps reusable
// lower-level components in this package while exposing search.Service as the main
// entrypoint for v1 search execution.
//
//   - Registry / SnapshotRegistry — resolve enabled SearchParameters from haistack-registry
//   - RegistryIndexer — search.Indexer implementation for pkg/core write-path indexing
//   - ParseQuery / ResolveQuery / BuildPlan — parse and plan FHIR query params
//   - StoreExecutor — execute typed index lookups via store.SearchQueryExecutor
//   - Service — high-level search execution returning bundle-ready Result values
//   - ReindexWorker / ReindexJobRunner / ReindexNotifier — rebuild index rows and enqueue on registry changes
//
// # MVP search parameters
//
// Supported parameter codes:
//
//   - _id, _lastUpdated
//   - identifier, name, phone, birthdate
//   - patient, subject, encounter
//   - status, date, code
//
// Unsupported in MVP (rejected explicitly): chained search, modifiers, prefixes,
// _include, _revinclude, _summary, _elements, full text, composite search.
//
// # Indexing
//
// RegistryIndexer evaluates each installed SearchParameter expression with pkg/fhirpath,
// normalizes extracted values into typed field keys (token.*, string.*, date.*,
// reference.*), and emits store.SearchIndexEntry rows consumed by store.SearchStore.
//
// # Query semantics
//
//   - Repeated parameters AND together
//   - Comma-separated values OR within one parameter occurrence
//   - _count and _offset provide offset paging; _sort supports _id and _lastUpdated
//
// # Backends
//
// Postgres is the first complete execution backend via store.SearchQueryExecutor.
// SQLite supports typed index persistence and LookupMatch for tests and embedded nodes,
// but full FHIR search execution is not required on SQLite in the MVP.
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
