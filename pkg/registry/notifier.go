package registry

import "context"

// SearchReindexNotifier schedules search reindex jobs when search parameters change.
type SearchReindexNotifier interface {
	ScheduleReindex(ctx context.Context, resourceTypes ...string) error
}
