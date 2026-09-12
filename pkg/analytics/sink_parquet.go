package analytics

import (
	"github.com/degoke/health-ai-stack/pkg/view"
)

// ExportRowCountSink optionally reports the row count from the most recent export write.
type ExportRowCountSink interface {
	RowSink
	LastExportRowCount() int
}

func sinkParquetLayout(sink RowSink) (view.ParquetLayout, bool) {
	switch s := sink.(type) {
	case *ParquetFileSink:
		return s.layout, true
	case LakehouseSink:
		if typed, ok := s.(*lakehouseSink); ok {
			return typed.cfg.ParquetLayout, true
		}
	case ManifestExportSink:
		if typed, ok := s.(*manifestExportSink); ok {
			return typed.parquetLayout, true
		}
	}
	return view.ParquetLayoutFlat, false
}

func lastExportRowCount(sink RowSink) int {
	if typed, ok := sink.(ExportRowCountSink); ok {
		return typed.LastExportRowCount()
	}
	return 0
}
