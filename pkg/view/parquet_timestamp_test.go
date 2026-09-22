package view_test

import (
	"testing"

	"github.com/degoke/health-ai-stack/pkg/view"
)

func TestParseTimestampEncoding(t *testing.T) {
	got, err := view.ParseTimestampEncoding("")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if got != view.TimestampEncodingInt64 {
		t.Fatalf("empty=%q, want int64", got)
	}
	got, err = view.ParseTimestampEncoding("int96")
	if err != nil {
		t.Fatalf("int96: %v", err)
	}
	if got != view.TimestampEncodingInt96 {
		t.Fatalf("int96=%q", got)
	}
	if _, err := view.ParseTimestampEncoding("julian"); err == nil {
		t.Fatal("expected error for unknown encoding")
	}
	if view.NormalizeTimestampEncoding("") != view.TimestampEncodingInt64 {
		t.Fatal("NormalizeTimestampEncoding empty should be int64")
	}
}
