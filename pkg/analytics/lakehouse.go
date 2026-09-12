package analytics

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// LakehouseArtifact describes one parquet object written by LakehouseSink.
type LakehouseArtifact struct {
	Partition string `json:"partition"`
	Location  string `json:"location"`
	RowCount  int    `json:"rowCount"`
}

// LakehouseConfig configures partitioned parquet export.
type LakehouseConfig struct {
	// Root writes parquet bytes to a single io.Writer when RootDir and Blob are unset.
	Root io.Writer
	// RootDir writes one parquet file per partition under a filesystem directory.
	RootDir string
	// Blob stores parquet objects when RootDir is unset.
	Blob store.BlobStore
	// BlobPrefix is prepended to blob object keys.
	BlobPrefix string
	// PartitionBy derives the partition path for a view result.
	PartitionBy func(*view.Result) string
	// ParquetLayout selects flat view columns or Parquet-on-FHIR nested resources.
	ParquetLayout view.ParquetLayout
	// Executor is required when ParquetLayout is fhir.
	Executor *view.Executor
	// Actor is forwarded to FHIR resource export authorization.
	Actor string
}

type lakehouseSink struct {
	cfg           LakehouseConfig
	partitionBy   func(*view.Result) string
	mu            sync.Mutex
	lastArtifacts []LakehouseArtifact
}

// NewLakehouseSink returns a sink that writes partitioned Apache Parquet files.
func NewLakehouseSink(cfg LakehouseConfig) LakehouseSink {
	partitionBy := cfg.PartitionBy
	if partitionBy == nil {
		partitionBy = defaultLakehousePartition
	}
	return &lakehouseSink{
		cfg:         cfg,
		partitionBy: partitionBy,
	}
}

func defaultLakehousePartition(result *view.Result) string {
	if result == nil {
		return "view=unknown"
	}
	return path.Join("view="+sanitizePartition(result.ViewName), "version="+sanitizePartition(result.Version))
}

func sanitizePartition(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "_")
	if value == "" {
		return "unknown"
	}
	return value
}

// LastArtifacts returns artifacts written by the most recent WriteRows call.
func (s *lakehouseSink) LastArtifacts() []LakehouseArtifact {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LakehouseArtifact, len(s.lastArtifacts))
	copy(out, s.lastArtifacts)
	return out
}

// LastExportRowCount implements ExportRowCountSink.
func (s *lakehouseSink) LastExportRowCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lastArtifacts) == 0 {
		return 0
	}
	return s.lastArtifacts[0].RowCount
}

func (s *lakehouseSink) WriteRows(ctx context.Context, result *view.Result) error {
	if s == nil {
		return fmt.Errorf("%w: lakehouse sink is required", ErrUnsupportedDestination)
	}
	if result == nil {
		return fmt.Errorf("analytics: nil view result")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	partition := s.partitionBy(result)
	filename := lakehouseFilename(result)
	artifact := LakehouseArtifact{Partition: partition}

	s.mu.Lock()
	defer s.mu.Unlock()

	var rowCount int
	var err error
	switch {
	case strings.TrimSpace(s.cfg.RootDir) != "":
		var location string
		location, rowCount, err = writeLakehouseParquetFile(ctx, s.cfg.RootDir, partition, filename, result, s.cfg.ParquetLayout, s.cfg.Executor, s.cfg.Actor)
		if err == nil {
			artifact.Location = location
		}
	case s.cfg.Blob != nil:
		var location string
		location, rowCount, err = writeLakehouseParquetBlob(ctx, s.cfg.Blob, s.cfg.BlobPrefix, partition, filename, result, s.cfg.ParquetLayout, s.cfg.Executor, s.cfg.Actor)
		if err == nil {
			artifact.Location = location
		}
	case s.cfg.Root != nil:
		rowCount, err = writeParquet(ctx, s.cfg.Root, result, s.cfg.ParquetLayout, s.cfg.Executor, s.cfg.Actor)
		if err == nil {
			artifact.Location = "stream:" + filename
		}
	default:
		return fmt.Errorf("%w: lakehouse writer is required", ErrUnsupportedDestination)
	}
	if err != nil {
		return err
	}
	artifact.RowCount = rowCount
	s.lastArtifacts = []LakehouseArtifact{artifact}
	return ctx.Err()
}

func lakehouseFilename(result *view.Result) string {
	viewName := "unknown"
	version := "1.0.0"
	if result != nil {
		if result.ViewName != "" {
			viewName = sanitizePartition(result.ViewName)
		}
		if result.Version != "" {
			version = sanitizePartition(result.Version)
		}
	}
	return fmt.Sprintf("%s-%s.parquet", viewName, version)
}

func writeLakehouseParquetFile(
	ctx context.Context,
	rootDir, partition, filename string,
	result *view.Result,
	layout view.ParquetLayout,
	executor *view.Executor,
	actor string,
) (string, int, error) {
	dir := filepath.Join(rootDir, filepath.FromSlash(partition))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create lakehouse partition dir: %w", err)
	}
	location := filepath.Join(dir, filename)
	file, err := os.Create(location)
	if err != nil {
		return "", 0, fmt.Errorf("create lakehouse parquet file: %w", err)
	}
	rowCount, err := writeParquet(ctx, file, result, layout, executor, actor)
	if err != nil {
		_ = file.Close()
		return "", rowCount, err
	}
	if err := file.Close(); err != nil {
		return "", rowCount, err
	}
	return location, rowCount, nil
}

func writeLakehouseParquetBlob(
	ctx context.Context,
	blob store.BlobStore,
	prefix, partition, filename string,
	result *view.Result,
	layout view.ParquetLayout,
	executor *view.Executor,
	actor string,
) (string, int, error) {
	tmp, err := os.CreateTemp("", "lakehouse-*.parquet")
	if err != nil {
		return "", 0, fmt.Errorf("create lakehouse temp parquet file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	rowCount, err := writeParquet(ctx, tmp, result, layout, executor, actor)
	if err != nil {
		_ = tmp.Close()
		return "", rowCount, err
	}
	if err := tmp.Close(); err != nil {
		return "", rowCount, err
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", rowCount, err
	}
	key := path.Join(strings.Trim(prefix, "/"), partition, filename)
	if err := blob.Put(ctx, store.BlobObject{
		Key:         key,
		ContentType: view.ParquetContentType,
		Size:        int64(len(data)),
		Data:        data,
	}); err != nil {
		return "", rowCount, fmt.Errorf("put lakehouse parquet blob: %w", err)
	}
	return key, rowCount, nil
}
