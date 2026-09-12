package parquetfhir_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/parquetfhir"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
)

func bundledPatientSD(t *testing.T) *validate.StructureDefinition {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", "Patient.json"))
	if err != nil {
		t.Fatalf("read Patient StructureDefinition: %v", err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	sd, ok := catalog.GetStructureDefinition(validate.BaseStructureDefinitionURL("Patient"))
	if !ok {
		t.Fatal("missing Patient StructureDefinition")
	}
	return sd
}

func TestWriteResourcesNestedPatientParquet(t *testing.T) {
	sd := bundledPatientSD(t)
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "example",
			"birthDate":    "1970-01-01",
			"name": []any{
				map[string]any{"family": "Doe", "given": []any{"Jane"}},
			},
			"telecom": []any{
				map[string]any{"system": "phone", "value": "555-0100"},
				map[string]any{"system": "email", "value": "jane@example.com"},
			},
		},
	}

	var buf bytes.Buffer
	if err := parquetfhir.WriteResources(&buf, sd, resources); err != nil {
		t.Fatalf("WriteResources: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 || string(data[:4]) != "PAR1" {
		t.Fatalf("expected PAR1 magic, got %q", string(data[:min(4, len(data))]))
	}

	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if file.NumRows() != 1 {
		t.Fatalf("rows=%d, want 1", file.NumRows())
	}
	if file.Schema().Name() != "Patient" {
		t.Fatalf("schema name=%q, want Patient", file.Schema().Name())
	}
}

func TestSchemaBuilderChoiceTypes(t *testing.T) {
	sd := bundledPatientSD(t)
	builder, err := parquetfhir.NewSchemaBuilder(sd)
	if err != nil {
		t.Fatalf("NewSchemaBuilder: %v", err)
	}
	builder.ObserveResource(map[string]any{
		"resourceType":         "Patient",
		"multipleBirthBoolean": false,
	})
	schema := builder.BuildSchema()
	if schema == nil {
		t.Fatal("expected schema")
	}
	if schema.Name() != "Patient" {
		t.Fatalf("schema name=%q", schema.Name())
	}
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
