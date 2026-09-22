package parquetfhir_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/degoke/haistack/pkg/parquetfhir"
	"github.com/degoke/haistack/pkg/validate"
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

func TestInteropSpecPatientINT96TimestampAnnotations(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{
			"resourceType": "Patient",
			"id":           "example",
			"birthDate":    "1970-01-01",
		},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	assertMetadataPhysicalType(t, data, format.Int96, "__birthDate_start")
	assertMetadataPhysicalType(t, data, format.Int96, "__birthDate_end")
	assertMetadataTimestampMillis(t, data, "__birthDate_start")
	assertMetadataTimestampMillis(t, data, "__birthDate_end")
	assertParquetGoCannotReadINT96Timestamp(t, data, "__birthDate_start")
	assertInt96MillisRoundTrip(t, data, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), "__birthDate_start")
	assertInt96MillisRoundTrip(t, data, time.Date(1970, 1, 1, 23, 59, 59, 999000000, time.UTC), "__birthDate_end")
}

func TestInteropSpecObservationINT96TimestampAnnotations(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType":      "Observation",
			"id":                "obs-spec",
			"status":            "final",
			"effectiveDateTime": "2022-02-10",
		},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	assertMetadataPhysicalType(t, data, format.Int96, "__effectiveDateTime_start")
	assertMetadataPhysicalType(t, data, format.Int96, "__effectiveDateTime_end")
	assertMetadataTimestampMillis(t, data, "__effectiveDateTime_start")
	assertMetadataTimestampMillis(t, data, "__effectiveDateTime_end")
	assertInt96MillisRoundTrip(t, data, time.Date(2022, 2, 10, 0, 0, 0, 0, time.UTC), "__effectiveDateTime_start")
}

func TestInteropSpecObservationPeriodStartINT96TimestampAnnotations(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType": "Observation",
			"id":           "obs-period",
			"status":       "final",
			"effectivePeriod": map[string]any{
				"start": "2022-02-10T00:00:00Z",
				"end":   "2022-02-11T00:00:00Z",
			},
		},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	assertMetadataPhysicalType(t, data, format.Int96, "effectivePeriod", "__start_start")
	assertMetadataPhysicalType(t, data, format.Int96, "effectivePeriod", "__start_end")
	assertMetadataPhysicalType(t, data, format.Int96, "effectivePeriod", "__end_start")
	assertMetadataPhysicalType(t, data, format.Int96, "effectivePeriod", "__end_end")
	assertMetadataTimestampMillis(t, data, "effectivePeriod", "__start_start")
	assertMetadataTimestampMillis(t, data, "effectivePeriod", "__start_end")
	assertInt96MillisRoundTrip(t, data, time.Date(2022, 2, 10, 0, 0, 0, 0, time.UTC), "effectivePeriod", "__start_start")
	assertInt96MillisRoundTrip(t, data, time.Date(2022, 2, 11, 0, 0, 0, 0, time.UTC), "effectivePeriod", "__end_start")
}

func TestINT96TimestampAnnotationsRowAlignedNulls(t *testing.T) {
	sd := bundledSD(t, "Patient")
	resources := []map[string]any{
		{"resourceType": "Patient", "id": "with-date", "birthDate": "1970-01-01"},
		{"resourceType": "Patient", "id": "without-date"},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	got, err := parquetfhir.ReadInt96MillisColumn(bytesReader(data), int64(len(data)), "__birthDate_start")
	if err != nil {
		t.Fatalf("ReadInt96MillisColumn: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2 row-aligned values", len(got))
	}
	if got[0] == nil || !got[0].Equal(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("row0=%v, want 1970-01-01 start", got[0])
	}
	if got[1] != nil {
		t.Fatalf("row1=%v, want nil annotation", got[1])
	}
}

func TestINT96TimestampAnnotationsDisambiguatesPeriodPaths(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType": "Observation",
			"id":           "obs-periods",
			"status":       "final",
			"effectivePeriod": map[string]any{
				"start": "2022-02-10T00:00:00Z",
			},
			"valuePeriod": map[string]any{
				"start": "2022-03-01T00:00:00Z",
			},
		},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	if _, err := parquetfhir.ReadInt96MillisColumn(bytesReader(data), int64(len(data)), "__start_start"); err == nil {
		t.Fatal("expected ambiguous __start_start to require a full path")
	}
	assertMetadataPhysicalType(t, data, format.Int96, "effectivePeriod", "__start_start")
	assertMetadataPhysicalType(t, data, format.Int96, "valuePeriod", "__start_start")
	assertMetadataTimestampMillis(t, data, "effectivePeriod", "__start_start")
	assertMetadataTimestampMillis(t, data, "valuePeriod", "__start_start")
	assertInt96MillisRoundTrip(t, data, time.Date(2022, 2, 10, 0, 0, 0, 0, time.UTC), "effectivePeriod", "__start_start")
	assertInt96MillisRoundTrip(t, data, time.Date(2022, 3, 1, 0, 0, 0, 0, time.UTC), "valuePeriod", "__start_start")
}

func TestINT96TimestampAnnotationsRepeatingComponentExceedsNumRows(t *testing.T) {
	sd := bundledSD(t, "Observation")
	resources := []map[string]any{
		{
			"resourceType": "Observation",
			"id":           "obs-components",
			"status":       "final",
			"component": []any{
				map[string]any{"valuePeriod": map[string]any{"start": "2022-01-01T00:00:00Z"}},
				map[string]any{"valuePeriod": map[string]any{"start": "2022-01-02T00:00:00Z"}},
			},
		},
		{"resourceType": "Observation", "id": "obs-empty", "status": "final"},
	}
	data := writeParquet(t, sd, resources, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if file.NumRows() != 2 {
		t.Fatalf("rows=%d, want 2", file.NumRows())
	}
	path := []string{"component", "list", "element", "valuePeriod", "__start_start"}
	assertMetadataPhysicalType(t, data, format.Int96, path...)
	got, err := parquetfhir.ReadInt96MillisColumn(bytesReader(data), int64(len(data)), path...)
	if err != nil {
		t.Fatalf("ReadInt96MillisColumn: %v", err)
	}
	if len(got) <= int(file.NumRows()) {
		t.Fatalf("len=%d, want definition-level length greater than NumRows=%d; columns=%v", len(got), file.NumRows(), file.Schema().Columns())
	}
	present := 0
	for _, ts := range got {
		if ts != nil {
			present++
		}
	}
	if present != 2 {
		t.Fatalf("present=%d, want 2 list-element timestamps, values=%v", present, got)
	}
}

func TestWriteResourcesStreamingINT96TimestampAnnotations(t *testing.T) {
	sd := bundledSD(t, "Patient")
	data := writeParquetStreaming(t, sd, nil, func(yield func(map[string]any) error) error {
		return yield(map[string]any{
			"resourceType": "Patient",
			"id":           "p1",
			"birthDate":    "1970-01-01",
		})
	}, parquetfhir.WithTimestampEncoding(parquetfhir.TimestampEncodingInt96))
	assertMetadataPhysicalType(t, data, format.Int96, "__birthDate_start")
	assertMetadataTimestampMillis(t, data, "__birthDate_start")
}

func TestBuildSchemaWithINT96Kind(t *testing.T) {
	sd := bundledSD(t, "Patient")
	builder, err := parquetfhir.NewSchemaBuilder(sd, nil)
	if err != nil {
		t.Fatalf("NewSchemaBuilder: %v", err)
	}
	builder.ObserveResource(map[string]any{
		"resourceType": "Patient",
		"birthDate":    "1970-01-01",
	})
	schema := builder.BuildSchemaWith(parquetfhir.TimestampEncodingInt96)
	leaf, ok := schema.Lookup("__birthDate_start")
	if !ok {
		t.Fatal("missing __birthDate_start")
	}
	if got := leaf.Node.Type().Kind(); got != parquet.Int96 {
		t.Fatalf("in-memory kind=%s, want INT96", got)
	}
	lt := leaf.Node.Type().LogicalType()
	if lt == nil {
		t.Fatal("missing logical type")
	}
	ts, ok := lt.Value.(*format.TimestampType)
	if !ok {
		t.Fatalf("logical type=%T, want TIMESTAMP", lt.Value)
	}
	if _, ok := ts.Unit.Value.(*format.MilliSeconds); !ok {
		t.Fatalf("TIMESTAMP unit=%v, want MILLIS", ts.Unit.Value)
	}
}

func TestParseTimestampEncoding(t *testing.T) {
	tests := []struct {
		raw     string
		want    parquetfhir.TimestampEncoding
		wantErr bool
	}{
		{raw: "", want: parquetfhir.TimestampEncodingInt64},
		{raw: "int64", want: parquetfhir.TimestampEncodingInt64},
		{raw: "INT96", want: parquetfhir.TimestampEncodingInt96},
		{raw: " int96 ", want: parquetfhir.TimestampEncodingInt96},
		{raw: "nanos", wantErr: true},
	}
	for _, tc := range tests {
		got, err := parquetfhir.ParseTimestampEncoding(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseTimestampEncoding(%q) err=nil, want error", tc.raw)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseTimestampEncoding(%q): %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("ParseTimestampEncoding(%q)=%q, want %q", tc.raw, got, tc.want)
		}
	}
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
			"resourceType":      "Observation",
			"id":                "obs-date",
			"status":            "final",
			"effectiveDateTime": "2022-02-10",
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
					"url":          "http://example.org/ext",
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

func writeParquetStreaming(t *testing.T, sd *validate.StructureDefinition, catalog validate.ProfileCatalog, fn func(yield func(map[string]any) error) error, opts ...parquetfhir.WriteOption) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := parquetfhir.WriteResourcesStreaming(context.Background(), &buf, sd, catalog, fn, opts...); err != nil {
		t.Fatalf("WriteResourcesStreaming: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 || string(data[:4]) != "PAR1" {
		t.Fatalf("expected PAR1 magic")
	}
	return data
}

func writeParquet(t *testing.T, sd *validate.StructureDefinition, resources []map[string]any, opts ...parquetfhir.WriteOption) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := parquetfhir.WriteResources(&buf, sd, nil, resources, opts...); err != nil {
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

func schemaElementByPath(t *testing.T, data []byte, path ...string) format.SchemaElement {
	t.Helper()
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	_, leaf, err := parquetfhir.ResolveInt96Column(file.Schema(), path...)
	if err != nil {
		t.Fatalf("ResolveInt96Column(%v): %v", path, err)
	}
	return leafSchemaElement(t, file.Metadata(), leaf.ColumnIndex)
}

func leafSchemaElement(t *testing.T, meta *format.FileMetaData, colIndex int) format.SchemaElement {
	t.Helper()
	leaf := 0
	for i := 1; i < len(meta.Schema); i++ {
		elem := meta.Schema[i]
		if !elem.Type.Valid {
			continue
		}
		if leaf == colIndex {
			return elem
		}
		leaf++
	}
	t.Fatalf("leaf column index %d not found in %#v", colIndex, meta.Schema)
	return format.SchemaElement{}
}

func assertMetadataPhysicalType(t *testing.T, data []byte, want format.Type, path ...string) {
	t.Helper()
	elem := schemaElementByPath(t, data, path...)
	if !elem.Type.Valid {
		t.Fatalf("column %v missing physical type", path)
	}
	if elem.Type.V != want {
		t.Fatalf("column %v physical type=%s, want %s", path, elem.Type.V, want)
	}
}

func assertMetadataTimestampMillis(t *testing.T, data []byte, path ...string) {
	t.Helper()
	elem := schemaElementByPath(t, data, path...)
	ts, ok := elem.LogicalType.Value.(*format.TimestampType)
	if !ok {
		t.Fatalf("column %v logical type=%T, want TIMESTAMP", path, elem.LogicalType.Value)
	}
	if !ts.IsAdjustedToUTC {
		t.Fatalf("column %v TIMESTAMP not UTC-adjusted", path)
	}
	if _, ok := ts.Unit.Value.(*format.MilliSeconds); !ok {
		t.Fatalf("column %v TIMESTAMP unit=%v, want MILLIS", path, ts.Unit.Value)
	}
}

func assertParquetGoCannotReadINT96Timestamp(t *testing.T, data []byte, path ...string) {
	t.Helper()
	file, err := parquet.OpenFile(bytesReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	resolved, leaf, err := parquetfhir.ResolveInt96Column(file.Schema(), path...)
	if err != nil {
		t.Fatalf("ResolveInt96Column(%v): %v", path, err)
	}
	col := columnByIndex(file.Root(), leaf.ColumnIndex)
	if col == nil {
		t.Fatalf("column %v (index %d) not found", resolved, leaf.ColumnIndex)
	}
	pages := col.Pages()
	defer func() { _ = pages.Close() }()
	_, err = pages.ReadPage()
	if err == nil {
		t.Fatal("parquet-go ReadPage succeeded; expected INT64 decode failure on INT96 TIMESTAMP pages")
	}
	if !strings.Contains(err.Error(), "INT64") && !strings.Contains(err.Error(), "size 12") {
		t.Fatalf("ReadPage error=%v, want INT64/size 12 mismatch", err)
	}
}

func assertInt96MillisRoundTrip(t *testing.T, data []byte, want time.Time, path ...string) {
	t.Helper()
	got, err := parquetfhir.ReadInt96MillisColumn(bytesReader(data), int64(len(data)), path...)
	if err != nil {
		t.Fatalf("ReadInt96MillisColumn(%v): %v", path, err)
	}
	if len(got) != 1 {
		t.Fatalf("ReadInt96MillisColumn(%v) len=%d, want 1", path, len(got))
	}
	if got[0] == nil || !got[0].Equal(want) {
		t.Fatalf("ReadInt96MillisColumn(%v)=%v, want %v", path, got[0], want)
	}
}

func columnByIndex(col *parquet.Column, index int) *parquet.Column {
	if col == nil {
		return nil
	}
	if col.Leaf() && col.Index() == index {
		return col
	}
	for _, child := range col.Columns() {
		if found := columnByIndex(child, index); found != nil {
			return found
		}
	}
	return nil
}

type bytesReader []byte

func (b bytesReader) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(b)) {
		return 0, io.EOF
	}
	return copy(p, b[off:]), nil
}
