// Command benchmarks runs vendor-neutral HAIStack research workloads.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
	"github.com/degoke/health-ai-stack/research/internal/researchutil"
	"gopkg.in/yaml.v3"
)

// WorkloadFile is a portable, vendor-neutral workload definition.
type WorkloadFile struct {
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Operations  []Operation `yaml:"operations" json:"operations"`
}

// Operation is one named measurement.
type Operation struct {
	Type         string `yaml:"type" json:"type"`
	ResourceType string `yaml:"resourceType" json:"resourceType"`
	ViewName     string `yaml:"viewName" json:"viewName"`
	Count        int    `yaml:"count" json:"count"`
}

// WorkloadResult is one workload's measurements.
type WorkloadResult struct {
	Name       string        `json:"name"`
	Operations int           `json:"operations"`
	Duration   time.Duration `json:"duration"`
	P50        time.Duration `json:"p50"`
	P95        time.Duration `json:"p95"`
	OpsPerSec  float64       `json:"opsPerSec"`
}

// BenchReport is the runner output.
type BenchReport struct {
	Track    string           `json:"track"`
	Size     string           `json:"size"`
	Seed     uint64           `json:"seed"`
	Patients int              `json:"patients"`
	Obs      int              `json:"observations"`
	Results  []WorkloadResult `json:"results"`
}

func main() {
	dumpDir := ""
	size := os.Getenv("HAISTACK_BENCH_SIZE")
	flag.StringVar(&dumpDir, "dump", "", "write generated synthetic FHIR JSON to DIR and exit")
	flag.StringVar(&size, "size", size, "dataset size: small, medium, or large")
	flag.Parse()
	if size == "" {
		size = SizeSmall
	}
	if dumpDir != "" {
		if err := Dump(dumpDir, size); err != nil {
			fmt.Fprintf(os.Stderr, "benchmarks: %v\n", err)
			os.Exit(1)
		}
		spec := sizeFor(size)
		summary := map[string]any{
			"track":        "A",
			"dump":         dumpDir,
			"size":         spec.Name,
			"seed":         datasetSeed,
			"patients":     spec.Patients,
			"observations": spec.Observations,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(summary); err != nil {
			fmt.Fprintf(os.Stderr, "benchmarks: %v\n", err)
			os.Exit(1)
		}
		return
	}
	report, err := Run(context.Background(), size)
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmarks: %v\n", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "benchmarks: %v\n", err)
		os.Exit(1)
	}
}

// Run generates the seeded dataset and executes bundled workloads.
func Run(ctx context.Context, size string) (*BenchReport, error) {
	patients, observations, err := Generate(size)
	if err != nil {
		return nil, err
	}
	resources := researchutil.NewMemoryResourceStore()
	for _, env := range append(append([]*types.ResourceEnvelope{}, patients...), observations...) {
		if err := resources.Create(ctx, env); err != nil {
			return nil, err
		}
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		return nil, err
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.ObservationView(), engine); err != nil {
		return nil, err
	}
	viewExec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		return nil, err
	}

	workloads, err := loadWorkloads()
	if err != nil {
		return nil, err
	}
	report := &BenchReport{
		Track:    "A",
		Size:     sizeFor(size).Name,
		Seed:     datasetSeed,
		Patients: len(patients),
		Obs:      len(observations),
	}
	for _, wl := range workloads {
		result, err := runWorkload(ctx, resources, viewExec, patients, observations, wl)
		if err != nil {
			return nil, fmt.Errorf("workload %s: %w", wl.Name, err)
		}
		report.Results = append(report.Results, result)
	}
	return report, nil
}

func runWorkload(ctx context.Context, resources store.ResourceStore, views *view.Executor, patients, observations []*types.ResourceEnvelope, wl WorkloadFile) (WorkloadResult, error) {
	var samples []time.Duration
	start := time.Now()
	ops := 0
	for _, op := range wl.Operations {
		n := op.Count
		if n <= 0 {
			n = 1
		}
		switch op.Type {
		case "read":
			pool := patients
			if op.ResourceType == "Observation" {
				pool = observations
			}
			if len(pool) == 0 {
				return WorkloadResult{}, fmt.Errorf("no %s resources", op.ResourceType)
			}
			for i := 0; i < n; i++ {
				res := pool[i%len(pool)]
				t0 := time.Now()
				if _, err := resources.Read(ctx, res.ResourceType, res.ID); err != nil {
					return WorkloadResult{}, err
				}
				samples = append(samples, time.Since(t0))
				ops++
			}
		case "scan-read":
			t0 := time.Now()
			ids, err := resources.ListIDs(ctx, op.ResourceType, n, 0)
			if err != nil {
				return WorkloadResult{}, err
			}
			samples = append(samples, time.Since(t0))
			ops++
			for _, id := range ids {
				t1 := time.Now()
				if _, err := resources.Read(ctx, op.ResourceType, id); err != nil {
					return WorkloadResult{}, err
				}
				samples = append(samples, time.Since(t1))
				ops++
			}
		case "view":
			for i := 0; i < n; i++ {
				t0 := time.Now()
				result, err := views.Execute(ctx, view.ExecuteRequest{ViewName: op.ViewName, Limit: 100})
				if err != nil {
					return WorkloadResult{}, err
				}
				if result.Total == 0 {
					return WorkloadResult{}, fmt.Errorf("view %s returned no rows", op.ViewName)
				}
				samples = append(samples, time.Since(t0))
				ops++
			}
		default:
			return WorkloadResult{}, fmt.Errorf("unknown operation %q", op.Type)
		}
	}
	elapsed := time.Since(start)
	p50, p95 := percentiles(samples)
	opsPerSec := 0.0
	if elapsed > 0 {
		opsPerSec = float64(ops) / elapsed.Seconds()
	}
	return WorkloadResult{
		Name:       wl.Name,
		Operations: ops,
		Duration:   elapsed,
		P50:        p50,
		P95:        p95,
		OpsPerSec:  opsPerSec,
	}, nil
}

func percentiles(samples []time.Duration) (p50, p95 time.Duration) {
	if len(samples) == 0 {
		return 0, 0
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 = sorted[len(sorted)*50/100]
	idx95 := len(sorted) * 95 / 100
	if idx95 >= len(sorted) {
		idx95 = len(sorted) - 1
	}
	return p50, sorted[idx95]
}

func loadWorkloads() ([]WorkloadFile, error) {
	names := []string{
		"patient-read.yaml",
		"observation-scan.yaml",
		"view-execute.yaml",
	}
	var out []WorkloadFile
	for _, name := range names {
		data, err := workloadsFS(name)
		if err != nil {
			return nil, err
		}
		var wl WorkloadFile
		if err := yaml.Unmarshal(data, &wl); err != nil {
			return nil, err
		}
		out = append(out, wl)
	}
	return out, nil
}
