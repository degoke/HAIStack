package parquetfhir

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/validate"
)

func bundledSD(t *testing.T, resourceType string) *validate.StructureDefinition {
	t.Helper()
	raw := bundledSDRaw(t, resourceType)
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

func bundledSDRaw(t *testing.T, resourceType string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "registry", "internal", "bundles", "r4", "structure-definitions", resourceType+".json"))
	if err != nil {
		t.Fatalf("read %s StructureDefinition: %v", resourceType, err)
	}
	return raw
}

func TestDateTimeRangePartialPrecision(t *testing.T) {
	tests := []struct {
		raw       string
		startYear int
		endYear   int
	}{
		{"2022", 2022, 2022},
		{"2022-02", 2022, 2022},
		{"2022-02-10", 2022, 2022},
		{"2022-02-10T12:05Z", 2022, 2022},
	}
	for _, tc := range tests {
		start, end, err := dateTimeRange(tc.raw)
		if err != nil {
			t.Fatalf("dateTimeRange(%q): %v", tc.raw, err)
		}
		if start.Year() != tc.startYear || end.Year() != tc.endYear {
			t.Fatalf("dateTimeRange(%q) years start=%v end=%v", tc.raw, start, end)
		}
		if end.Before(start) {
			t.Fatalf("dateTimeRange(%q) end before start", tc.raw)
		}
	}
}

func TestDateTimeRangeFullTimestampEndMillisecond(t *testing.T) {
	start, end, err := dateTimeRange("2014-06-01T12:05:00Z")
	if err != nil {
		t.Fatalf("dateTimeRange: %v", err)
	}
	wantEnd := start.Add(time.Second - time.Millisecond)
	if !end.Equal(wantEnd) {
		t.Fatalf("end=%v want %v", end, wantEnd)
	}
}

func TestDateTimeRangeInvalidYearRejected(t *testing.T) {
	if _, _, err := dateTimeRange("abcd"); err == nil {
		t.Fatal("expected invalid year error")
	}
}

func TestEnrichMapSkipsInvalidDecimalAnnotation(t *testing.T) {
	idx, err := newElementIndex(bundledSD(t, "Patient"))
	if err != nil {
		t.Fatalf("newElementIndex: %v", err)
	}
	row := map[string]any{
		"extension": []any{
			map[string]any{
				"url":          "http://example.org/ext",
				"valueDecimal": "not-a-decimal",
			},
		},
	}
	if err := enrichMap(row, "Patient", idx, ""); err != nil {
		t.Fatalf("enrichMap: %v", err)
	}
	ext := row["extension"].([]any)[0].(map[string]any)
	if _, ok := ext["__valueDecimal_numeric"]; ok {
		t.Fatal("expected invalid decimal annotation to be skipped")
	}
}

func TestResolveFieldTypeExtensionValueInteger(t *testing.T) {
	idx, err := newElementIndex(bundledSD(t, "Patient"))
	if err != nil {
		t.Fatalf("newElementIndex: %v", err)
	}
	typ := idx.resolveFieldType("Patient.extension.valueInteger", "valueInteger", "Extension")
	if typ != "integer" {
		t.Fatalf("type=%q, want integer", typ)
	}
}

func TestResolveFieldTypePeriodStart(t *testing.T) {
	idx, err := newElementIndex(bundledSD(t, "Observation"))
	if err != nil {
		t.Fatalf("newElementIndex: %v", err)
	}
	typ := idx.resolveFieldType("Observation.effectivePeriod.start", "start", "Period")
	if typ != "dateTime" {
		t.Fatalf("type=%q, want dateTime", typ)
	}
}
