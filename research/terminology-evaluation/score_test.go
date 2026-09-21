package main

import "testing"

func TestIndependentGoldAndDefectivePipeline(t *testing.T) {
	report, err := Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Gold < 1 {
		t.Fatal("gold set is empty")
	}
	if report.GoldMap == nil || report.PipelineMap == nil {
		t.Fatal("expected goldMap and pipelineMap scores")
	}

	g := report.GoldMap
	if g.Exact == 0 || g.Narrow == 0 || g.Broad == 0 || g.Unmatched == 0 {
		t.Fatalf("gold map missing equivalence classes: exact=%d narrow=%d broad=%d unmatched=%d",
			g.Exact, g.Narrow, g.Broad, g.Unmatched)
	}
	if g.TruePos != report.Gold || g.FalsePos != 0 || g.FalseNeg != 0 {
		t.Fatalf("gold ConceptMap must reproduce independent translations: tp=%d fp=%d fn=%d gold=%d mismatches=%+v",
			g.TruePos, g.FalsePos, g.FalseNeg, report.Gold, g.Mismatches)
	}

	p := report.PipelineMap
	if p.F1 >= 1 || p.FalsePos < 1 || p.FalseNeg < 1 {
		t.Fatalf("pipeline map must be independent of gold (expect F1<1 with FP and FN): f1=%v tp=%d fp=%d fn=%d mismatches=%+v",
			p.F1, p.TruePos, p.FalsePos, p.FalseNeg, p.Mismatches)
	}
	wantKind := map[string]string{
		"hr":      "fp", // wrong LOINC (9279-1 instead of 8867-4)
		"dbp":     "fn", // missing element
		"fever":   "fp", // equivalent instead of wider
		"unknown": "fp", // falsely mapped
	}
	gotKind := map[string]string{}
	for _, m := range p.Mismatches {
		gotKind[m.Source] = m.Kind
	}
	for src, kind := range wantKind {
		if gotKind[src] != kind {
			t.Fatalf("pipeline mismatch for %s: got %q want %q (mismatches=%+v)", src, gotKind[src], kind, p.Mismatches)
		}
	}
	if p.TruePos != 8 || p.FalsePos != 3 || p.FalseNeg != 1 {
		t.Fatalf("unexpected pipeline confusion counts: tp=%d fp=%d fn=%d (want 8/3/1)", p.TruePos, p.FalsePos, p.FalseNeg)
	}
	if p.MapVersion == g.MapVersion {
		t.Fatalf("pipeline map version %q must differ from gold %q", p.MapVersion, g.MapVersion)
	}

	if report.AuditEvents != report.Gold {
		t.Fatalf("audit events = %d, gold = %d", report.AuditEvents, report.Gold)
	}
	for _, prov := range report.Provenance {
		if prov.ConceptMapURL == "" || prov.ConceptMapVersion == "" || prov.SourceSystemVersion == "" || prov.TranslatedAt.IsZero() {
			t.Fatalf("incomplete provenance: %+v", prov)
		}
		if prov.ConceptMapVersion != p.MapVersion {
			t.Fatalf("provenance version %q, pipeline %q", prov.ConceptMapVersion, p.MapVersion)
		}
	}
}
