package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/cql"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type recordingMeasureService struct {
	last hahttp.MeasureEvaluateRequest
}

func (r *recordingMeasureService) EvaluateMeasure(_ context.Context, req hahttp.MeasureEvaluateRequest) (*types.ResourceEnvelope, error) {
	r.last = req
	return types.NewJSONCodec().ParseJSON("MeasureReport", []byte(`{
		"resourceType": "MeasureReport",
		"status": "complete",
		"type": "individual",
		"measure": "http://example.org/Measure/Adult"
	}`))
}

func TestEvaluateMeasureOperation(t *testing.T) {
	svc := &recordingMeasureService{}
	h := newTestHandler(t, hahttp.Config{
		ResourceService:        &fakeResourceService{},
		MeasureEvaluateService: svc,
	})
	rec := doRequest(t, h, http.MethodGet, "/fhir/Measure/adult/$evaluate-measure?periodStart=2020-01-01&periodEnd=2021-01-01&reportType=individual&subject=Patient/ada", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if svc.last.ID != "adult" {
		t.Fatalf("id=%q", svc.last.ID)
	}
	var report struct {
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.ResourceType != "MeasureReport" {
		t.Fatalf("type=%q", report.ResourceType)
	}
}

func TestEvaluateMeasureNotImplemented(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}})
	rec := doRequest(t, h, http.MethodGet, "/fhir/Measure/adult/$evaluate-measure?periodStart=2020-01-01&periodEnd=2021-01-01", nil)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCoreMeasureServiceIndividual(t *testing.T) {
	eng, err := cql.NewEngine(cql.Config{})
	if err != nil {
		t.Fatal(err)
	}
	src := `
library Adult version '1.0.0'
using FHIR version '4.0.1'
parameter "Measurement Period" Interval<DateTime>
context Patient
define "Initial Population": true
define "Denominator": AgeInYears() >= 18
define "Numerator": Patient.gender = 'female'
`
	lib, err := eng.ParseLibrary(src)
	if err != nil {
		t.Fatal(err)
	}
	lib.URL = "http://example.org/Library/Adult"
	measure, err := types.NewJSONCodec().ParseJSON("Measure", []byte(`{
		"resourceType": "Measure",
		"id": "adult",
		"url": "http://example.org/Measure/Adult",
		"library": ["http://example.org/Library/Adult"],
		"scoring": {"coding": [{"code": "proportion"}]},
		"group": [{"population": [
			{"code": {"coding": [{"code": "initial-population"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Initial Population"}},
			{"code": {"coding": [{"code": "denominator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Denominator"}},
			{"code": {"coding": [{"code": "numerator"}]}, "criteria": {"language": "text/cql.identifier", "expression": "Numerator"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	patient, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
		"resourceType": "Patient", "id": "ada", "gender": "female", "birthDate": "2000-01-15"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	resources := memStore{
		"Measure/adult": measure,
		"Patient/ada":   patient,
	}
	svc := hahttp.CoreMeasureService{
		Engine:    eng,
		Libraries: cql.StaticLibraries{"http://example.org/Library/Adult": lib},
		Resources: resources,
	}
	h := newTestHandler(t, hahttp.Config{
		ResourceService:        &fakeResourceService{},
		MeasureEvaluateService: svc,
	})
	rec := doRequest(t, h, http.MethodGet, "/fhir/Measure/adult/$evaluate-measure?periodStart=2020-01-01&periodEnd=2021-01-01&reportType=individual&subject=Patient/ada", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var report map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["resourceType"] != "MeasureReport" || report["type"] != "individual" {
		t.Fatalf("report: %#v", report)
	}
}

type memStore map[string]*types.ResourceEnvelope

func (m memStore) Create(context.Context, *types.ResourceEnvelope) error { return nil }
func (m memStore) Update(context.Context, *types.ResourceEnvelope) error { return nil }
func (m memStore) Delete(context.Context, string, string) error          { return nil }
func (m memStore) Exists(context.Context, string, string) (bool, error)  { return false, nil }
func (m memStore) Read(_ context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	return m[resourceType+"/"+id], nil
}
func (m memStore) ListIDs(_ context.Context, resourceType string, _, _ int) ([]string, error) {
	var ids []string
	prefix := resourceType + "/"
	for k := range m {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			ids = append(ids, k[len(prefix):])
		}
	}
	return ids, nil
}
