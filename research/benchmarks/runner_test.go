package benchmarks_test

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/research/benchmarks"
)

func TestGenerateDeterministic(t *testing.T) {
	a, err := benchmarks.Generate(benchmarks.DefaultSeed, benchmarks.SizeSmall)
	if err != nil {
		t.Fatal(err)
	}
	b, err := benchmarks.Generate(benchmarks.DefaultSeed, benchmarks.SizeSmall)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Patients) != 10 || len(a.Observations) != 20 {
		t.Fatalf("small size = %d patients %d obs", len(a.Patients), len(a.Observations))
	}
	if a.Patients[0].Hash != b.Patients[0].Hash {
		t.Fatal("generator is not deterministic")
	}
}

func TestHAIStackWorkloads(t *testing.T) {
	file, err := benchmarks.LoadWorkloads(benchmarks.DefaultWorkloadPath())
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := benchmarks.NewHAIStackAdapter()
	if err != nil {
		t.Fatal(err)
	}
	report, err := benchmarks.Run(context.Background(), adapter, benchmarks.SizeSmall, benchmarks.DefaultSeed, file.Workloads)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != len(file.Workloads) {
		t.Fatalf("results = %d, want %d", len(report.Results), len(file.Workloads))
	}
	for _, r := range report.Results {
		if r.Error != "" && !r.Skipped {
			t.Errorf("%s: %s", r.Workload, r.Error)
		}
		if r.Workload == "crud-read" && r.Operations != 10 {
			t.Errorf("crud-read operations = %d", r.Operations)
		}
	}
}
