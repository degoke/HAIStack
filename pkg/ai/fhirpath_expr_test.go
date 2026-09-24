package ai

import "testing"

func TestNormalizeFHIRPathForCompile_KeywordDiv(t *testing.T) {
	got := NormalizeFHIRPathForCompile("text.div")
	if got != NarrativeDivFHIRPath {
		t.Fatalf("got %q want %q", got, NarrativeDivFHIRPath)
	}
}

func TestSegmentPathFromFHIRPathExpr_Backticks(t *testing.T) {
	got := SegmentPathFromFHIRPathExpr(NarrativeDivFHIRPath)
	if got != NarrativeDivSegmentPath {
		t.Fatalf("got %q want %q", got, NarrativeDivSegmentPath)
	}
}
