package view_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/view"
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
