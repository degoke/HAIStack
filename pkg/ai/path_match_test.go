package ai

import "testing"

func TestPathHasSuffixNoMiddleSegmentFalsePositive(t *testing.T) {
	if pathHasSuffix("something.name.other", "name") {
		t.Fatal("pathHasSuffix should not match suffix in the middle of the path")
	}
	if !pathHasSuffix("patient.name.family", "name.family") {
		t.Fatal("expected suffix match at path end")
	}
}
