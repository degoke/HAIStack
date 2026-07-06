package registry

import "context"

// SearchReindexNotifier schedules search reindex jobs when search parameters change.
type SearchReindexNotifier interface {
	ScheduleReindex(ctx context.Context, resourceTypes ...string) error
}

// SearchParameterReindexNotifier schedules reindex jobs for one SearchParameter definition.
type SearchParameterReindexNotifier interface {
	SearchReindexNotifier
	ScheduleSearchParameterReindex(ctx context.Context, canonicalURL, version string, resourceTypes ...string) error
}
