package analytics

import (
	"bytes"
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
	artifact := LakehouseArtifact{Partition: partition, RowCount: len(result.Rows)}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case strings.TrimSpace(s.cfg.RootDir) != "":
		location, err := writeLakehouseParquetFile(s.cfg.RootDir, partition, filename, result)
		if err != nil {
			return err
		}
		artifact.Location = location
	case s.cfg.Blob != nil:
		location, err := writeLakehouseParquetBlob(ctx, s.cfg.Blob, s.cfg.BlobPrefix, partition, filename, result)
		if err != nil {
			return err
		}
		artifact.Location = location
	case s.cfg.Root != nil:
		if err := view.WriteParquetResult(s.cfg.Root, result); err != nil {
			return fmt.Errorf("write lakehouse parquet: %w", err)
		}
		artifact.Location = "stream:" + filename
	default:
		return fmt.Errorf("%w: lakehouse writer is required", ErrUnsupportedDestination)
	}

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

func writeLakehouseParquetFile(rootDir, partition, filename string, result *view.Result) (string, error) {
	dir := filepath.Join(rootDir, filepath.FromSlash(partition))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create lakehouse partition dir: %w", err)
	}
	location := filepath.Join(dir, filename)
	file, err := os.Create(location)
	if err != nil {
		return "", fmt.Errorf("create lakehouse parquet file: %w", err)
	}
	if err := view.WriteParquetResult(file, result); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return location, nil
}

func writeLakehouseParquetBlob(
	ctx context.Context,
	blob store.BlobStore,
	prefix, partition, filename string,
	result *view.Result,
) (string, error) {
	var buf bytes.Buffer
	if err := view.WriteParquetResult(&buf, result); err != nil {
		return "", err
	}
	key := path.Join(strings.Trim(prefix, "/"), partition, filename)
	if err := blob.Put(ctx, store.BlobObject{
		Key:         key,
		ContentType: view.ParquetContentType,
		Size:        int64(buf.Len()),
		Data:        buf.Bytes(),
	}); err != nil {
		return "", fmt.Errorf("put lakehouse parquet blob: %w", err)
	}
	return key, nil
}
