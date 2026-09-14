package viewtest

import (
	"context"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/testkit/fixtures"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/view"
)

// PatientSummaryPatients holds Jane and John envelopes for patient_summary_view tests.
type PatientSummaryPatients struct {
	Jane *types.ResourceEnvelope
	John *types.ResourceEnvelope
}

// DefaultPatientSummaryPatients returns Jane and John without LastUpdated overrides.
func DefaultPatientSummaryPatients(t *testing.T) PatientSummaryPatients {
	t.Helper()
	return PatientSummaryPatients{
		Jane: fixtures.PatientJane(t),
		John: fixtures.PatientJohn(t),
	}
}

// IncrementalPatientSummaryPatients returns Jane and John with distinct LastUpdated
// values for incremental export and watermark tests.
func IncrementalPatientSummaryPatients(t *testing.T) PatientSummaryPatients {
	t.Helper()
	jane := fixtures.PatientJane(t)
	jane.LastUpdated = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	john := fixtures.PatientJohn(t)
	john.LastUpdated = time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	return PatientSummaryPatients{Jane: jane, John: john}
}

// NewPatientSummaryExecutor seeds patients and returns an executor for patient_summary_view.
func NewPatientSummaryExecutor(t *testing.T, patients PatientSummaryPatients) *view.Executor {
	t.Helper()
	resources := storetest.NewResourceStore()
	if err := resources.Seed(context.Background(), patients.Jane, patients.John); err != nil {
		t.Fatalf("viewtest: seed resources: %v", err)
	}
	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		t.Fatalf("viewtest: NewEngine: %v", err)
	}
	reg := view.NewRegistry()
	if _, err := reg.Register(view.PatientSummaryView(), engine); err != nil {
		t.Fatalf("viewtest: register patient_summary_view: %v", err)
	}
	exec, err := view.NewExecutor(view.Config{
		Resources: resources,
		Engine:    engine,
		Registry:  reg,
	})
	if err != nil {
		t.Fatalf("viewtest: NewExecutor: %v", err)
	}
	return exec
}
