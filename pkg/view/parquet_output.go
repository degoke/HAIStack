package view

import (
	"context"
	"fmt"
	"io"
)

// WriteParquetOutput writes flat view rows or Parquet-on-FHIR resources to w.
func WriteParquetOutput(ctx context.Context, w io.Writer, result *Result, execReq ExecuteRequest, layout ParquetLayout, exec *Executor) (int, error) {
	if layout == ParquetLayoutFHIR {
		if exec == nil {
			return 0, fmt.Errorf("view: executor is required for Parquet-on-FHIR layout")
		}
		rowCount, stats, err := WriteParquetFHIRExport(ctx, w, exec, execReq)
		applyMatchingResourceStats(result, stats)
		return rowCount, err
	}
	if result == nil {
		return 0, fmt.Errorf("view: nil result")
	}
	if err := WriteParquetResult(w, result); err != nil {
		return 0, err
	}
	return len(result.Rows), nil
}

func applyMatchingResourceStats(result *Result, stats MatchingResourceStats) {
	if result == nil {
		return
	}
	if stats.SourceResourceType != "" {
		result.Metadata.SourceResourceType = stats.SourceResourceType
	}
	result.Metadata.Scanned = stats.Scanned
	result.Metadata.Filtered = stats.Filtered
}
