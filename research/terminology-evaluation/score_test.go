package main

import (
	"encoding/json"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/conceptmap"
)

func TestScorerDetectsPlantedDefects(t *testing.T) {
	report, err := Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if report.Gold < 1 {
		t.Fatal("gold set is empty")
	}
	var gold GoldFile
	if err := json.Unmarshal(goldJSON, &gold); err != nil {
		t.Fatal(err)
	}
	cmap, err := conceptmap.ParseMap(goldConceptMapJSON)
	if err != nil {
		t.Fatal(err)
	}
	if gold.ConceptMap != cmap.URL || gold.ConceptMapVersion != cmap.Version {
		t.Fatalf("gold translations metadata %s@%s != control map %s@%s",
			gold.ConceptMap, gold.ConceptMapVersion, cmap.URL, cmap.Version)
	}
	if report.GoldMap == nil || report.FixtureMap == nil {
		t.Fatal("expected goldMap and fixtureMap scores")
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

	p := report.FixtureMap
	if p.F1 >= 1 || p.FalsePos < 1 || p.FalseNeg < 1 {
		t.Fatalf("defective fixture must score F1<1 with FP and FN: f1=%v tp=%d fp=%d fn=%d mismatches=%+v",
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
			t.Fatalf("fixture mismatch for %s: got %q want %q (mismatches=%+v)", src, gotKind[src], kind, p.Mismatches)
		}
	}
	if p.TruePos != 8 || p.FalsePos != 3 || p.FalseNeg != 1 {
		t.Fatalf("unexpected fixture confusion counts: tp=%d fp=%d fn=%d (planted 8/3/1)", p.TruePos, p.FalsePos, p.FalseNeg)
	}
	if p.MapVersion == g.MapVersion {
		t.Fatalf("fixture map version %q must differ from gold %q", p.MapVersion, g.MapVersion)
	}

	if report.AuditEvents != report.Gold {
		t.Fatalf("audit events = %d, gold = %d", report.AuditEvents, report.Gold)
	}
	for _, prov := range report.Provenance {
		if prov.ConceptMapURL == "" || prov.ConceptMapVersion == "" || prov.SourceSystemVersion == "" || prov.TranslatedAt.IsZero() {
			t.Fatalf("incomplete provenance: %+v", prov)
		}
		if prov.ConceptMapVersion != p.MapVersion {
			t.Fatalf("provenance version %q, fixture %q", prov.ConceptMapVersion, p.MapVersion)
		}
	}
}
