package view_test

import (
	"testing"

	"github.com/degoke/haistack/pkg/view"
)

func TestIsOperationOutputFormat(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"parquet", true},
		{"ndjson", true},
		{"nd-json", true},
		{"csv", true},
		// json is a FHIR envelope (ParseOutputFormat -> FormatJSON), not an artifact.
		{"json", false},
		{"xml", false},
		{"", false},
		{"unknown", false},
	} {
		if got := view.IsOperationOutputFormat(tc.raw); got != tc.want {
			t.Fatalf("IsOperationOutputFormat(%q)=%v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestParseOutputFormatJSONIsFHIREnvelopeNotArtifact(t *testing.T) {
	if got := view.ParseOutputFormat("json"); got != view.FormatJSON {
		t.Fatalf("ParseOutputFormat(json)=%q, want %q", got, view.FormatJSON)
	}
	if view.IsOperationOutputFormat("json") {
		t.Fatal("IsOperationOutputFormat(json)=true, want false (json uses FHIR negotiation)")
	}
}
