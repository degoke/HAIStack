package store

import (
	"context"
	"time"
)

// ReportingColumn describes one output column in a reporting table schema.
type ReportingColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ReportingTableMeta records the schema and refresh metadata for one view snapshot.
type ReportingTableMeta struct {
	ViewName    string            `json:"viewName"`
	ViewVersion string            `json:"viewVersion"`
	Columns     []ReportingColumn `json:"columns"`
	RowCount    int               `json:"rowCount"`
	RefreshedAt time.Time         `json:"refreshedAt"`
}

// ReportingTableStore persists tenant-scoped analytics reporting tables as
// queryable row sets keyed by view identity. Full refresh replaces all rows for
// a view version atomically.
type ReportingTableStore interface {
	Refresh(ctx context.Context, meta ReportingTableMeta, rows []map[string]any) error
	QueryRows(ctx context.Context, viewName, viewVersion string) ([]map[string]any, error)
	GetMeta(ctx context.Context, viewName, viewVersion string) (*ReportingTableMeta, error)
}
