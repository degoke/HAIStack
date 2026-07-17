package http_test

import (
	"context"
	"net/http"
	"testing"

	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type fakeSDCService struct{}

func (fakeSDCService) Populate(context.Context, hahttp.SDCRequest) (*types.ResourceEnvelope, error) {
	return &types.ResourceEnvelope{ResourceType: "QuestionnaireResponse", JSON: []byte(`{"resourceType":"QuestionnaireResponse","status":"in-progress"}`)}, nil
}
func (fakeSDCService) Validate(context.Context, hahttp.SDCRequest) (*types.OperationOutcome, error) {
	return &types.OperationOutcome{ResourceType: "OperationOutcome"}, nil
}
func (fakeSDCService) Extract(context.Context, hahttp.SDCRequest) (*types.ResourceEnvelope, error) {
	return &types.ResourceEnvelope{ResourceType: "Bundle", JSON: []byte(`{"resourceType":"Bundle","type":"transaction"}`)}, nil
}
func (fakeSDCService) Assemble(context.Context, hahttp.SDCRequest) (*types.ResourceEnvelope, error) {
	return &types.ResourceEnvelope{ResourceType: "Questionnaire", JSON: []byte(`{"resourceType":"Questionnaire"}`)}, nil
}
func (fakeSDCService) Adaptive(context.Context, string, hahttp.SDCRequest) (*types.ResourceEnvelope, error) {
	return &types.ResourceEnvelope{ResourceType: "QuestionnaireResponse", JSON: []byte(`{"resourceType":"QuestionnaireResponse"}`)}, nil
}

func TestSDCOperationRouting(t *testing.T) {
	h := newTestHandler(t, hahttp.Config{ResourceService: &fakeResourceService{}, SDCService: fakeSDCService{}})
	rec := doRequest(t, h, http.MethodPost, "/fhir/Questionnaire/$populate", []byte(`{"resourceType":"Parameters"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("populate status=%d body=%s", rec.Code, rec.Body.String())
	}
	rec = doRequest(t, h, http.MethodPost, "/fhir/QuestionnaireResponse/$validate", []byte(`{"resourceType":"QuestionnaireResponse"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", rec.Code, rec.Body.String())
	}
}
