package http

import "testing"

func TestParseRouteViewExportFileAcceptsArtifactFilename(t *testing.T) {
	route, err := parseRoute("/fhir", "/fhir/ViewDefinition/$viewdefinition-export/files/export-job-1/patient_summary_view-1.0.0.ndjson")
	if err != nil {
		t.Fatalf("parseRoute: %v", err)
	}
	if route.kind != routeViewExportFile {
		t.Fatalf("kind=%v", route.kind)
	}
	if route.filename != "patient_summary_view-1.0.0.ndjson" {
		t.Fatalf("filename=%q", route.filename)
	}
}

func TestParseRouteBulkExportFileAcceptsArtifactFilename(t *testing.T) {
	route, err := parseRoute("/fhir", "/fhir/$export/files/export-job-1/Patient.ndjson")
	if err != nil {
		t.Fatalf("parseRoute: %v", err)
	}
	if route.kind != routeBulkExportFile {
		t.Fatalf("kind=%v", route.kind)
	}
	if route.filename != "Patient.ndjson" {
		t.Fatalf("filename=%q", route.filename)
	}
}

func TestParseRouteRejectsInvalidExportFilename(t *testing.T) {
	cases := []string{
		"/fhir/ViewDefinition/$viewdefinition-export/files/export-job-1/..",
		"/fhir/ViewDefinition/$viewdefinition-export/files/export-job-1/../escape.ndjson",
		"/fhir/ViewDefinition/$viewdefinition-export/files/export-job-1/%2e%2e",
		"/fhir/$export/files/export-job-1/Patient%2fndjson",
		"/fhir/$export/files/export-job-1/",
	}
	for _, path := range cases {
		if _, err := parseRoute("/fhir", path); err == nil {
			t.Fatalf("expected error for path %q", path)
		}
	}
}

func TestValidateExportFilename(t *testing.T) {
	valid := []string{
		"Patient.ndjson",
		"Patient.error.ndjson",
		"patient_summary_view-1.0.0.ndjson",
		"out.csv",
	}
	for _, name := range valid {
		if err := validateExportFilename(name); err != nil {
			t.Fatalf("validateExportFilename(%q): %v", name, err)
		}
	}

	invalid := []string{"", ".", "..", "../x", "bad/name", `bad\name`, "has space.ndjson"}
	for _, name := range invalid {
		if err := validateExportFilename(name); err == nil {
			t.Fatalf("expected error for filename %q", name)
		}
	}
}
