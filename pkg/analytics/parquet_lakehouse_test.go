package analytics_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/degoke/haistack/pkg/analytics"
	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/testkit/viewtest"
	"github.com/degoke/haistack/pkg/validate"
	"github.com/degoke/haistack/pkg/view"
)

func bundledPatientCatalog(t *testing.T) validate.MemoryProfileCatalog {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Patient.json"))
	if err != nil {
		t.Fatalf("read Patient StructureDefinition: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	return catalog
}

func newFHIRExecutor(t *testing.T) *view.Executor {
	t.Helper()
	return viewtest.NewPatientSummaryExecutorWithConfig(t, viewtest.DefaultPatientSummaryPatients(t), viewtest.ExecutorConfig{
		ProfileCatalog: bundledPatientCatalog(t),
	})
}

func newFHIRExecutorWithStore(t *testing.T, resources *memResourceStore) *view.Executor {
	t.Helper()
	return viewtest.NewPatientSummaryExecutorWithConfig(t, viewtest.PatientSummaryPatients{}, viewtest.ExecutorConfig{
		Resources:      resources,
		ProfileCatalog: bundledPatientCatalog(t),
	})
}

func TestLakehouseSinkWritesPartitionedParquetFile(t *testing.T) {
	root := t.TempDir()
	sink := analytics.NewLakehouseSink(analytics.LakehouseConfig{RootDir: root})
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Columns:  []view.ColumnInfo{{Name: "patient_id", Type: "string"}},
		Rows:     []map[string]any{{"patient_id": "p1"}},
	}
	if err := sink.WriteRows(context.Background(), result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	typed, ok := sink.(interface {
		LastArtifacts() []analytics.LakehouseArtifact
	})
	if !ok {
		t.Fatal("expected LastArtifacts on lakehouse sink")
	}
	artifacts := typed.LastArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%v", artifacts)
	}
	data, err := os.ReadFile(artifacts[0].Location)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet file on disk")
	}
	if !strings.Contains(artifacts[0].Location, filepath.Join("view="+analytics.ViewPatientSummary, "version=1.0.0")) {
		t.Fatalf("location=%q", artifacts[0].Location)
	}
}

func TestLakehouseSinkWritesFHIRLayoutParquet(t *testing.T) {
	root := t.TempDir()
	exec := newFHIRExecutor(t)
	sink := analytics.NewLakehouseSink(analytics.LakehouseConfig{
		RootDir:       root,
		ParquetLayout: view.ParquetLayoutFHIR,
		Executor:      exec,
	})
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
	}
	if err := sink.WriteRows(context.Background(), result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	typed, ok := sink.(interface {
		LastArtifacts() []analytics.LakehouseArtifact
	})
	if !ok {
		t.Fatal("expected LastArtifacts on lakehouse sink")
	}
	artifacts := typed.LastArtifacts()
	if len(artifacts) != 1 {
		t.Fatalf("artifacts=%v", artifacts)
	}
	if artifacts[0].RowCount != 2 {
		t.Fatalf("rowCount=%d, want 2", artifacts[0].RowCount)
	}
	data, err := os.ReadFile(artifacts[0].Location)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !view.IsParquetFile(data) {
		t.Fatal("expected parquet file on disk")
	}
}

func TestManifestExportSinkWritesFHIRLayoutParquet(t *testing.T) {
	exec := newFHIRExecutor(t)
	var buf bytes.Buffer
	sink := analytics.NewManifestExportSink(analytics.ManifestExportConfig{
		Root:          &buf,
		Format:        analytics.FormatParquet,
		ParquetLayout: view.ParquetLayoutFHIR,
		Executor:      exec,
	})
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
	}
	if err := sink.WriteRows(context.Background(), result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if !view.IsParquetFile(buf.Bytes()) {
		t.Fatal("expected parquet binary output")
	}
}

func TestLakehouseSinkWritesParquetBlob(t *testing.T) {
	blobs := &memBlobStore{objects: map[string]store.BlobObject{}}
	sink := analytics.NewLakehouseSink(analytics.LakehouseConfig{
		Blob:       blobs,
		BlobPrefix: "exports",
	})
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Columns:  []view.ColumnInfo{{Name: "patient_id", Type: "string"}},
		Rows:     []map[string]any{{"patient_id": "p1"}},
	}
	if err := sink.WriteRows(context.Background(), result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if len(blobs.objects) != 1 {
		t.Fatalf("objects=%d", len(blobs.objects))
	}
	for _, obj := range blobs.objects {
		if obj.ContentType != view.ParquetContentType {
			t.Fatalf("contentType=%q", obj.ContentType)
		}
		if !view.IsParquetFile(obj.Data) {
			t.Fatal("expected parquet blob payload")
		}
	}
}

func TestLakehouseSinkStreamsParquetBlobWithoutFullFilePut(t *testing.T) {
	blobs := &streamOnlyMemBlobStore{objects: map[string]store.BlobObject{}}
	sink := analytics.NewLakehouseSink(analytics.LakehouseConfig{
		Blob:       blobs,
		BlobPrefix: "exports",
	})
	result := &view.Result{
		ViewName: analytics.ViewPatientSummary,
		Version:  "1.0.0",
		Columns:  []view.ColumnInfo{{Name: "patient_id", Type: "string"}},
		Rows:     []map[string]any{{"patient_id": "p1"}},
	}
	if err := sink.WriteRows(context.Background(), result); err != nil {
		t.Fatalf("WriteRows: %v", err)
	}
	if blobs.putCalls != 0 {
		t.Fatalf("putCalls=%d, want 0 (must stream via PutStream, not os.ReadFile + Put)", blobs.putCalls)
	}
	if blobs.putStreamCalls != 1 {
		t.Fatalf("putStreamCalls=%d, want 1", blobs.putStreamCalls)
	}
	if len(blobs.objects) != 1 {
		t.Fatalf("objects=%d", len(blobs.objects))
	}
	for _, obj := range blobs.objects {
		if !view.IsParquetFile(obj.Data) {
			t.Fatal("expected parquet blob payload")
		}
		if obj.Size != int64(len(obj.Data)) || obj.Size == 0 {
			t.Fatalf("size=%d data=%d", obj.Size, len(obj.Data))
		}
	}
}

type memBlobStore struct {
	objects map[string]store.BlobObject
}

func (m *memBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	m.objects[obj.Key] = obj
	return nil
}

func (m *memBlobStore) PutStream(_ context.Context, key, contentType string, size int64, r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if size <= 0 {
		size = int64(len(data))
	}
	return m.Put(context.Background(), store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        data,
	})
}

func (m *memBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	obj, ok := m.objects[key]
	if !ok {
		return nil, nil
	}
	copy := obj
	return &copy, nil
}

func (m *memBlobStore) Head(_ context.Context, key string) (*store.BlobObject, error) {
	return m.Get(context.Background(), key)
}

func (m *memBlobStore) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

type streamOnlyMemBlobStore struct {
	mu             sync.Mutex
	objects        map[string]store.BlobObject
	putCalls       int
	putStreamCalls int
}

func (m *streamOnlyMemBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.putCalls++
	if len(obj.Data) > 0 {
		return fmt.Errorf("buffered Put of %d bytes is not allowed", len(obj.Data))
	}
	m.objects[obj.Key] = obj
	return nil
}

func (m *streamOnlyMemBlobStore) PutStream(_ context.Context, key, contentType string, size int64, r io.Reader) error {
	m.mu.Lock()
	m.putStreamCalls++
	m.mu.Unlock()
	w := &sliceWriter{}
	buf := make([]byte, 32*1024)
	if _, err := io.CopyBuffer(w, r, buf); err != nil {
		return err
	}
	if size <= 0 {
		size = int64(len(w.b))
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = store.BlobObject{
		Key:         key,
		ContentType: contentType,
		Size:        size,
		Data:        w.b,
	}
	return nil
}

type sliceWriter struct {
	b []byte
}

func (w *sliceWriter) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

func (m *streamOnlyMemBlobStore) Get(_ context.Context, key string) (*store.BlobObject, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	obj, ok := m.objects[key]
	if !ok {
		return nil, nil
	}
	copy := obj
	return &copy, nil
}

func (m *streamOnlyMemBlobStore) Head(ctx context.Context, key string) (*store.BlobObject, error) {
	return m.Get(ctx, key)
}

func (m *streamOnlyMemBlobStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}
