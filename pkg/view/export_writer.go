package view

import (
	"bytes"
	"context"
	"fmt"
	"sync"
)

// MemoryExportWriter stores exported artifacts in memory for tests and edge mode.
type MemoryExportWriter struct {
	mu    sync.Mutex
	files map[string][]byte
}

// NewMemoryExportWriter returns an in-memory ExportWriter.
func NewMemoryExportWriter() *MemoryExportWriter {
	return &MemoryExportWriter{files: make(map[string][]byte)}
}

// WriteExport encodes one view result using the requested export format.
func (w *MemoryExportWriter) WriteExport(ctx context.Context, filename string, result *Result, format OutputFormat) error {
	if w == nil {
		return fmt.Errorf("view: export writer is nil")
	}
	var buf bytes.Buffer
	if err := writeFormattedExport(ctx, &buf, result, format); err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files[filename] = append([]byte(nil), buf.Bytes()...)
	return nil
}

// Get returns one exported artifact by filename.
func (w *MemoryExportWriter) Get(filename string) ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	data, ok := w.files[filename]
	return data, ok
}
