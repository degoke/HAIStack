package analytics

import (
	"context"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/view"
)

func writeParquet(ctx context.Context, w io.Writer, result *view.Result, layout view.ParquetLayout, executor *view.Executor, actor string) (int, error) {
	if w == nil {
		return 0, fmt.Errorf("%w: parquet writer is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return 0, fmt.Errorf("analytics: nil view result")
	}
	execReq := view.ExecRequestForExport(result, actor)
	return view.WriteParquetOutput(ctx, w, result, execReq, layout, executor)
}
