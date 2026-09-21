package main

import (
	"testing"
)

func TestGenerateSeedStable(t *testing.T) {
	aP, aO, err := Generate(SizeSmall)
	if err != nil {
		t.Fatal(err)
	}
	bP, bO, err := Generate(SizeSmall)
	if err != nil {
		t.Fatal(err)
	}
	if len(aP) != 20 || len(aO) != 40 {
		t.Fatalf("small size patients=%d obs=%d", len(aP), len(aO))
	}
	if aP[0].Hash != bP[0].Hash || aO[0].Hash != bO[0].Hash {
		t.Fatal("seeded dataset is not stable")
	}
}

func TestRunSmallWorkloads(t *testing.T) {
	report, err := Run(t.Context(), SizeSmall)
	if err != nil {
		t.Fatal(err)
	}
	if report.Track != "A" {
		t.Fatalf("track = %s", report.Track)
	}
	if len(report.Results) != 3 {
		t.Fatalf("results = %d", len(report.Results))
	}
	for _, r := range report.Results {
		if r.Operations == 0 {
			t.Fatalf("workload %s performed no operations", r.Name)
		}
	}
}

func TestLoadWorkloadsEmbedded(t *testing.T) {
	wls, err := loadWorkloads()
	if err != nil {
		t.Fatal(err)
	}
	if len(wls) != 3 {
		t.Fatalf("workloads = %d", len(wls))
	}
}
