package main

import "testing"

func TestTerminologyGoldSet(t *testing.T) {
	report, err := Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Gold < 1 {
		t.Fatal("gold set is empty")
	}
	if report.Exact == 0 || report.Narrow == 0 || report.Broad == 0 || report.Unmatched == 0 {
		t.Fatalf("expected all equivalence classes, got exact=%d narrow=%d broad=%d unmatched=%d",
			report.Exact, report.Narrow, report.Broad, report.Unmatched)
	}
	if report.F1 != 1 {
		t.Fatalf("expected perfect gold agreement, precision=%v recall=%v f1=%v fp=%d fn=%d",
			report.Precision, report.Recall, report.F1, report.FalsePos, report.FalseNeg)
	}
	if report.AuditEvents != report.Gold {
		t.Fatalf("audit events = %d, gold = %d", report.AuditEvents, report.Gold)
	}
	for _, p := range report.Provenance {
		if p.ConceptMapURL == "" || p.ConceptMapVersion == "" || p.SourceSystemVersion == "" || p.TranslatedAt.IsZero() {
			t.Fatalf("incomplete provenance: %+v", p)
		}
	}
}
