package parquetfhir_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/parquetfhir"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

func bundledSD(t *testing.T, resourceType string) *validate.StructureDefinition {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", resourceType+".json"))
	if err != nil {
		t.Fatalf("read %s StructureDefinition: %v", resourceType, err)
	}
	catalog, err := validate.LoadProfileCatalogFromJSON([][]byte{raw})
	if err != nil {
		t.Fatalf("LoadProfileCatalogFromJSON: %v", err)
	}
	sd, ok := catalog.GetStructureDefinition(validate.BaseStructureDefinitionURL(resourceType))
	if !ok {
		t.Fatalf("missing %s StructureDefinition", resourceType)
	}
	return sd
}

func TestInteropSpecPatientExampleSchema(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "example",
			"birthDate":    "1970-01-01",
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "resourceType")
	assertColumn(t, schema, "id")
	assertColumn(t, schema, "birthDate")
	assertColumnKind(t, schema, parquet.ByteArray, "birthDate")
	assertColumn(t, schema, "__birthDate_start")
	assertColumn(t, schema, "__birthDate_end")
	assertColumnKind(t, schema, parquet.Int64, "__birthDate_start")
	assertColumnKind(t, schema, parquet.Int64, "__birthDate_end")
	assertColumnTimestampMillis(t, schema, "__birthDate_start")
	assertColumnTimestampMillis(t, schema, "__birthDate_end")
}

func TestInteropSpecObservationExampleSchema(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType":      "Observation",
			"id":                "obs-spec",
			"status":            "final",
			"effectiveDateTime": "2022-02-10",
			"valueQuantity": map[string]any{
				"value":  36.5,
				"unit":   "C",
				"system": "http://unitsofmeasure.org",
				"code":   "Cel",
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "effectiveDateTime")
	assertColumnKind(t, schema, parquet.ByteArray, "effectiveDateTime")
	assertColumn(t, schema, "__effectiveDateTime_start")
	assertColumnKind(t, schema, parquet.Int64, "__effectiveDateTime_start")
	assertColumnTimestampMillis(t, schema, "__effectiveDateTime_start")
	assertColumn(t, schema, "valueQuantity", "value")
	assertColumnKind(t, schema, parquet.ByteArray, "valueQuantity", "value")
	assertColumn(t, schema, "valueQuantity", "__value_numeric")
	assertColumnKind(t, schema, parquet.FixedLenByteArray, "valueQuantity", "__value_numeric")
	assertColumnDecimal(t, schema, 38, 6, "valueQuantity", "__value_numeric")
	assertColumn(t, schema, "__valueQuantity_canonical", "value")
	assertColumnKind(t, schema, parquet.ByteArray, "__valueQuantity_canonical", "value")
}

func TestWriteResourcesSkipsInvalidAnnotationDuringExport(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "bad-anno",
			"birthDate":    "not-a-date",
			"extension": []any{
				map[string]any{
					"url":          "http://example.org/ext",
					"valueDecimal": "not-a-decimal",
				},
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "id")
	assertColumn(t, schema, "birthDate")
}

func TestWriteResourcesNestedPatientParquet(t *testing.T) {
	sd := bundledSD(t, "Patient")
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

	data := writeParquet(t, sd, resources)
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
	assertColumn(t, file.Schema(), "id")
	assertColumn(t, file.Schema(), "name", "list", "element", "family")
	assertColumn(t, file.Schema(), "telecom", "list", "element", "value")
}

func TestWriteResourcesObservationPartialEffectiveDateTime(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType":       "Observation",
			"id":                 "obs-date",
			"status":             "final",
			"effectiveDateTime":  "2022-02-10",
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "effectiveDateTime")
	assertColumn(t, schema, "__effectiveDateTime_start")
	assertColumn(t, schema, "__effectiveDateTime_end")
}

func TestWriteResourcesExtensionValueDecimalAnnotation(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "ext-dec",
			"extension": []any{
				map[string]any{
					"url":          "http://example.org/ext",
					"valueDecimal": "12.345678",
				},
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "extension", "list", "element", "valueDecimal")
	assertColumn(t, schema, "extension", "list", "element", "__valueDecimal_numeric")
}

func TestWriteResourcesExtensionValueInteger(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "ext-int",
			"extension": []any{
				map[string]any{
					"url":           "http://example.org/ext",
					"valueInteger": 42,
				},
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "extension", "list", "element", "valueInteger")
}

func TestWriteResourcesPatientContainedOrganization(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "p-contained",
			"contained": []any{
				map[string]any{
					"resourceType": "Organization",
					"id":           "org-1",
					"name":         "Acme Health",
				},
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "contained", "list", "element", "name")
	assertColumn(t, schema, "contained", "list", "element", "resourceType")
}

func TestWriteResourcesObservationQuantityAnnotations(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType": "Observation",
			"id":           "obs-1",
			"status":       "final",
			"valueQuantity": map[string]any{
				"value":  36.5,
				"unit":   "C",
				"system": "http://unitsofmeasure.org",
				"code":   "Cel",
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "valueQuantity", "value")
	assertColumn(t, schema, "valueQuantity", "__value_numeric")
	assertColumn(t, schema, "__valueQuantity_canonical", "value")
	assertColumn(t, schema, "__valueQuantity_canonical", "code")
}

func TestWriteResourcesObservationQuantityCanonicalKelvin(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType": "Observation",
			"id":           "obs-temp",
			"status":       "final",
			"valueQuantity": map[string]any{
				"value":  36.5,
				"unit":   "C",
				"system": "http://unitsofmeasure.org",
				"code":   "Cel",
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "__valueQuantity_canonical", "value")
	assertColumn(t, schema, "__valueQuantity_canonical", "unit")
}

func TestWriteResourcesExtensionAndPrimitiveWrapper(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "ext-1",
			"birthDate":    "1970-01-01",
			"_birthDate": map[string]any{
				"id": "bd-1",
				"extension": []any{
					map[string]any{
						"url":           "http://hl7.org/fhir/StructureDefinition/patient-birthTime",
						"valueDateTime": "1970-01-01T00:00:00Z",
					},
				},
			},
			"extension": []any{
				map[string]any{
					"url": "http://hl7.org.au/fhir/StructureDefinition/indigenous-status",
					"valueCoding": map[string]any{
						"system": "https://healthterminologies.gov.au/fhir/CodeSystem/australian-indigenous-status-1",
						"code":   "1",
					},
				},
			},
		},
	}
	data := writeParquet(t, sd, resources)
	schema := mustOpenSchema(t, data)
	assertColumn(t, schema, "extension", "list", "element", "url")
	assertColumn(t, schema, "_birthDate", "id")
	assertColumn(t, schema, "__birthDate_start")
	assertColumn(t, schema, "__birthDate_end")
}

func TestSchemaBuilderChoiceTypes(t *testing.T) {
	sd := bundledSD(t, "Patient")
	builder, err := parquetfhir.NewSchemaBuilder(sd, nil)
	if err != nil {
		t.Fatalf("NewSchemaBuilder: %v", err)
	}
	builder.ObserveResource(map[string]any{
		"resourceType":         "Patient",
		"multipleBirthBoolean": false,
	})
	schema := builder.BuildSchema()
	columns := schema.Columns()
	found := false
	for _, col := range columns {
		if len(col) == 1 && col[0] == "multipleBirthBoolean" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected multipleBirthBoolean column, got %#v", columns)
	}
}

func TestWriteResourcesStreamingSinglePassCollection(t *testing.T) {
	sd := bundledSD(t, "Patient")
	pass := 0
	resources := []map[string]any{
		{"resourceType": "Patient", "id": "p1"},
		{"resourceType": "Patient", "id": "p2"},
	}
	data := writeParquetStreaming(t, sd, nil, func(yield func(map[string]any) error) error {
		pass++
		for _, resource := range resources {
			if err := yield(resource); err != nil {
				return err
			}
		}
		return nil
	})
	if pass != 1 {
		t.Fatalf("passes=%d, want 1", pass)
	}
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if file.NumRows() != 2 {
		t.Fatalf("rows=%d, want 2", file.NumRows())
	}
}

func writeParquetStreaming(t *testing.T, sd *validate.StructureDefinition, catalog validate.ProfileCatalog, fn func(yield func(map[string]any) error) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := parquetfhir.WriteResourcesStreaming(context.Background(), &buf, sd, catalog, fn); err != nil {
		t.Fatalf("WriteResourcesStreaming: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 || string(data[:4]) != "PAR1" {
		t.Fatalf("expected PAR1 magic")
	}
	return data
}

func writeParquet(t *testing.T, sd *validate.StructureDefinition, resources []map[string]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := parquetfhir.WriteResources(&buf, sd, nil, resources); err != nil {
		t.Fatalf("WriteResources: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 || string(data[:4]) != "PAR1" {
		t.Fatalf("expected PAR1 magic")
	}
	return data
}

func mustOpenSchema(t *testing.T, data []byte) *parquet.Schema {
	t.Helper()
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return file.Schema()
}

func assertColumn(t *testing.T, schema *parquet.Schema, parts ...string) {
	t.Helper()
	for _, col := range schema.Columns() {
		if len(col) < len(parts) {
			continue
		}
		match := true
		for i, part := range parts {
			if col[i] != part {
				match = false
				break
			}
		}
		if match {
			return
		}
	}
	t.Fatalf("column not found: %v in %#v", parts, schema.Columns())
}

func assertColumnKind(t *testing.T, schema *parquet.Schema, want parquet.Kind, parts ...string) {
	t.Helper()
	leaf, ok := schema.Lookup(parts...)
	if !ok {
		t.Fatalf("column not found for kind check: %v", parts)
	}
	if got := leaf.Node.Type().Kind(); got != want {
		t.Fatalf("column %v kind=%s, want %s", parts, got, want)
	}
}

func assertColumnTimestampMillis(t *testing.T, schema *parquet.Schema, parts ...string) {
	t.Helper()
	leaf, ok := schema.Lookup(parts...)
	if !ok {
		t.Fatalf("column not found for timestamp check: %v", parts)
	}
	lt := leaf.Node.Type().LogicalType()
	if lt == nil {
		t.Fatalf("column %v missing logical type", parts)
	}
	ts, ok := lt.Value.(*format.TimestampType)
	if !ok {
		t.Fatalf("column %v logical type=%T, want TIMESTAMP", parts, lt.Value)
	}
	if !ts.IsAdjustedToUTC {
		t.Fatalf("column %v TIMESTAMP not UTC-adjusted", parts)
	}
	if _, ok := ts.Unit.Value.(*format.MilliSeconds); !ok {
		t.Fatalf("column %v TIMESTAMP unit=%v, want MILLIS", parts, ts.Unit.Value)
	}
}

func assertColumnDecimal(t *testing.T, schema *parquet.Schema, precision, scale int, parts ...string) {
	t.Helper()
	leaf, ok := schema.Lookup(parts...)
	if !ok {
		t.Fatalf("column not found for decimal check: %v", parts)
	}
	lt := leaf.Node.Type().LogicalType()
	if lt == nil {
		t.Fatalf("column %v missing logical type", parts)
	}
	dec, ok := lt.Value.(*format.DecimalType)
	if !ok {
		t.Fatalf("column %v logical type=%T, want DECIMAL", parts, lt.Value)
	}
	if dec.Precision != int32(precision) || dec.Scale != int32(scale) {
		t.Fatalf("column %v DECIMAL(%d,%d), want DECIMAL(%d,%d)", parts, dec.Precision, dec.Scale, precision, scale)
	}
	if leaf.Node.Type().Length() != 16 {
		t.Fatalf("column %v decimal length=%d, want 16", parts, leaf.Node.Type().Length())
	}
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}
