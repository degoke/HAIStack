package analytics_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/analytics"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/degoke/health-ai-stack/pkg/view"
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
	resources := newMemResourceStore()
	resources.Seed(t, patientJane(t), patientJohn(t))
	return newFHIRExecutorWithStore(t, resources)
}

func newFHIRExecutorWithStore(t *testing.T, resources *memResourceStore) *view.Executor {
	t.Helper()
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("register: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources:      resources,
		Engine:         engine,
		Registry:       reg,
		ProfileCatalog: bundledPatientCatalog(t),
	})
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	return exec
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

type memBlobStore struct {
	objects map[string]store.BlobObject
}

func (m *memBlobStore) Put(_ context.Context, obj store.BlobObject) error {
	m.objects[obj.Key] = obj
	return nil
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
