package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/degoke/health-ai-stack/pkg/view"
)

// NDJSONSink writes one JSON object per line.
type NDJSONSink struct {
	w io.Writer
}

// NewNDJSONSink returns a sink that writes NDJSON to w.
func NewNDJSONSink(w io.Writer) RowSink {
	return &NDJSONSink{w: w}
}

func (s *NDJSONSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil || s.w == nil {
		return fmt.Errorf("%w: ndjson writer is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return fmt.Errorf("analytics: nil view result")
	}
	for _, row := range result.Rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		b, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("marshal ndjson row: %w", err)
		}
		if _, err := s.w.Write(b); err != nil {
			return fmt.Errorf("write ndjson row: %w", err)
		}
		if _, err := s.w.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write ndjson newline: %w", err)
		}
	}
	return ctx.Err()
}
