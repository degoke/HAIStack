package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

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

func TestCoreMeasureServiceGroupIndividualAndExtraParameters(t *testing.T) {
	eng, err := cql.NewEngine(cql.Config{
		Now: func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	src := `
library Adult version '1.0.0'
using FHIR version '4.0.1'
parameter "Measurement Period" Interval<DateTime>
parameter "MinAge" Integer default 0
context Patient
define "Initial Population": true
define "Denominator": AgeInYears() >= MinAge
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
	child, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
		"resourceType": "Patient", "id": "kid", "gender": "male", "birthDate": "2020-01-01"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	group, err := types.NewJSONCodec().ParseJSON("Group", []byte(`{
		"resourceType": "Group",
		"id": "panel",
		"member": [
			{"entity": {"reference": "Patient/ada"}},
			{"entity": {"reference": "Patient/kid"}}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	resources := memStore{
		"Measure/adult": measure,
		"Patient/ada":   patient,
		"Patient/kid":   child,
		"Group/panel":   group,
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
	rec := doRequest(t, h, http.MethodGet, "/fhir/Measure/adult/$evaluate-measure?periodStart=2020-01-01&periodEnd=2021-01-01&reportType=individual&subject=Group/panel", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("group individual status=%d body=%s", rec.Code, rec.Body.String())
	}
	var groupReport map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &groupReport); err != nil {
		t.Fatal(err)
	}
	if groupReport["type"] != "summary" {
		t.Fatalf("Group subject with individual reportType should evaluate members as summary, got %#v", groupReport["type"])
	}

	body := []byte(`{
		"resourceType": "Parameters",
		"parameter": [
			{"name": "periodStart", "valueDate": "2020-01-01"},
			{"name": "periodEnd", "valueDate": "2021-01-01"},
			{"name": "reportType", "valueCode": "individual"},
			{"name": "subject", "valueString": "Patient/ada"},
			{"name": "MinAge", "valueInteger": 100}
		]
	}`)
	rec = doRequest(t, h, http.MethodPost, "/fhir/Measure/adult/$evaluate-measure", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("extra params status=%d body=%s", rec.Code, rec.Body.String())
	}
	var paramReport map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &paramReport); err != nil {
		t.Fatal(err)
	}
	groups, _ := paramReport["group"].([]any)
	g0, _ := groups[0].(map[string]any)
	pops, _ := g0["population"].([]any)
	den := 0
	for _, raw := range pops {
		p, _ := raw.(map[string]any)
		code, _ := p["code"].(map[string]any)
		coding, _ := code["coding"].([]any)
		c0, _ := coding[0].(map[string]any)
		if c0["code"] == "denominator" {
			switch n := p["count"].(type) {
			case float64:
				den = int(n)
			case int:
				den = n
			}
		}
	}
	if den != 0 {
		t.Fatalf("MinAge parameter was not forwarded; denominator=%d report=%#v", den, paramReport)
	}

	empty := hahttp.CoreMeasureService{
		Engine:    eng,
		Libraries: cql.StaticLibraries{"http://example.org/Library/Adult": lib},
		Resources: memStore{},
	}
	h = newTestHandler(t, hahttp.Config{
		ResourceService:        &fakeResourceService{},
		MeasureEvaluateService: empty,
	})
	rec = doRequest(t, h, http.MethodGet, "/fhir/Measure/adult/$evaluate-measure?periodStart=2020-01-01&periodEnd=2021-01-01&reportType=individual&subject=Patient/ada", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil measure envelope status=%d body=%s", rec.Code, rec.Body.String())
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
