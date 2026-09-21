package main

import (
	"bytes"
	"os"
	"path/filepath"
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
	var viewLimit int
	for _, wl := range wls {
		if wl.Name != "view-execute" {
			continue
		}
		if len(wl.Operations) == 0 {
			t.Fatal("view-execute has no operations")
		}
		viewLimit = wl.Operations[0].Limit
	}
	if viewLimit != 100 {
		t.Fatalf("view-execute limit = %d, want 100 (portable page size)", viewLimit)
	}
}

func TestDumpWritesJSON(t *testing.T) {
	dir := t.TempDir()
	if err := Dump(dir, SizeSmall); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Patient", "pat-0000.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Observation", "obs-0000.json")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"seed": 11`)) && !bytes.Contains(raw, []byte(`"seed":11`)) {
		t.Fatalf("manifest = %s", raw)
	}
}
