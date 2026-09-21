package benchmarks

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Result is one workload measurement. There is no combined score.
type Result struct {
	Workload   string        `json:"workload"`
	Adapter    string        `json:"adapter"`
	Operations int           `json:"operations"`
	Rows       int           `json:"rows,omitempty"`
	Elapsed    time.Duration `json:"elapsed"`
	Skipped    bool          `json:"skipped,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// Report is a per-workload evaluation.
type Report struct {
	Size    string   `json:"size"`
	Seed    int64    `json:"seed"`
	Adapter string   `json:"adapter"`
	Results []Result `json:"results"`
}

// DefaultWorkloadPath returns the published YAML catalogue.
func DefaultWorkloadPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "testdata/workloads.yaml"
	}
	return filepath.Join(filepath.Dir(file), "testdata", "workloads.yaml")
}

// Run executes every workload against adapter using a seeded dataset.
func Run(ctx context.Context, adapter Adapter, size string, seed int64, workloads []Workload) (Report, error) {
	ds, err := Generate(seed, size)
	if err != nil {
		return Report{}, err
	}
	all := make([]*types.ResourceEnvelope, 0, len(ds.Patients)+len(ds.Observations))
	all = append(all, ds.Patients...)
	all = append(all, ds.Observations...)
	if err := adapter.Load(ctx, all); err != nil {
		return Report{}, err
	}
	report := Report{Size: size, Seed: ds.Seed, Adapter: adapter.Name()}
	for _, wl := range workloads {
		report.Results = append(report.Results, runWorkload(ctx, adapter, ds, wl))
	}
	return report, nil
}

func runWorkload(ctx context.Context, adapter Adapter, ds *Dataset, wl Workload) Result {
	start := time.Now()
	res := Result{Workload: wl.Name, Adapter: adapter.Name()}
	var err error
	switch wl.Operation {
	case "read":
		res.Operations = len(ds.Patients)
		for _, p := range ds.Patients {
			if err = adapter.Read(ctx, "Patient", p.ID); err != nil {
				break
			}
		}
	case "view":
		res.Operations = 1
		res.Rows, err = adapter.RunView(ctx, wl.ViewName, wl.ViewVersion)
	case "ai-run-view":
		res.Operations = 1
		res.Rows, err = adapter.RunAIView(ctx, wl.ViewName, wl.ViewVersion)
		if err != nil && wl.Optional {
			res.Skipped = true
			res.Error = err.Error()
			res.Elapsed = time.Since(start)
			return res
		}
	default:
		err = fmt.Errorf("unknown operation %q", wl.Operation)
	}
	res.Elapsed = time.Since(start)
	if err != nil {
		res.Error = err.Error()
	}
	return res
}
