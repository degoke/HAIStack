package analytics

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// ReportingTarget writes view execution results into a Postgres reporting table.
type ReportingTarget struct {
	store store.ReportingTableStore
	now   func() time.Time
}

// NewReportingTarget returns a target that refreshes store.ReportingTableStore.
func NewReportingTarget(s store.ReportingTableStore) *ReportingTarget {
	return &ReportingTarget{
		store: s,
		now:   time.Now,
	}
}

// Write persists the full view result via a reporting table refresh.
func (t *ReportingTarget) Write(ctx context.Context, result *view.Result) error {
	if t == nil || t.store == nil {
		return fmt.Errorf("%w: reporting store is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return fmt.Errorf("analytics: nil view result")
	}

	columns := make([]store.ReportingColumn, len(result.Columns))
	for i, col := range result.Columns {
		columns[i] = store.ReportingColumn{Name: col.Name, Type: col.Type}
	}

	meta := store.ReportingTableMeta{
		ViewName:    result.ViewName,
		ViewVersion: result.Version,
		Columns:     columns,
		RowCount:    len(result.Rows),
		RefreshedAt: t.now().UTC(),
	}
	return t.store.Refresh(ctx, meta, result.Rows)
}
