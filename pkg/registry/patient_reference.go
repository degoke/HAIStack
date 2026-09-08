package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	"github.com/degoke/health-ai-stack/pkg/types"
	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
)

// PatientReferenceResolver extracts the patient id linked to a FHIR resource
// using the same SearchParameter selection as patient-scoped search.
type PatientReferenceResolver struct {
	mu       sync.RWMutex
	Snapshot *Snapshot
	Engine   fhirpath.Engine
}

// SetSnapshot replaces the compiled snapshot used for patient resolution.
func (r *PatientReferenceResolver) SetSnapshot(snapshot *Snapshot) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.Snapshot = snapshot
	r.mu.Unlock()
}

// PatientIDForResource returns the patient id when the resource is linked to
// exactly one patient via the registry patient scope SearchParameter.
func (r *PatientReferenceResolver) PatientIDForResource(ctx context.Context, resourceType string, resource *types.ResourceEnvelope) (string, bool, error) {
	if r == nil || resource == nil {
		return "", false, nil
	}
	if resourceType == "" {
		resourceType = resource.ResourceType
	}
	if resourceType == "Patient" {
		id := strings.TrimSpace(resource.ID)
		if id == "" {
			id = patientIDFromJSON(resource.JSON)
		}
		if id == "" {
			return "", false, nil
		}
		return id, true, nil
	}
	r.mu.RLock()
	snapshot := r.Snapshot
	engine := r.Engine
	r.mu.RUnlock()
	if snapshot == nil || engine == nil {
		return "", false, fmt.Errorf("registry patient reference resolver is not configured")
	}
	param, ok := snapshot.PatientSearchParameter(resourceType)
	if !ok || param.Expression == "" {
		return "", false, nil
	}
	values, err := engine.Eval(ctx, param.Expression, resource)
	if err != nil {
		return "", false, fmt.Errorf("evaluate patient scope expression: %w", err)
	}
	id := firstPatientID(values)
	return id, id != "", nil
}

func patientIDFromJSON(raw []byte) string {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.ID)
}

func firstPatientID(values []fhirpath.Value) string {
	for _, value := range values {
		if id := patientIDFromValue(value); id != "" {
			return id
		}
	}
	return ""
}

func patientIDFromValue(value fhirpath.Value) string {
	switch ref := value.Raw().(type) {
	case *dtpb.Reference:
		return patientIDFromReference(ref)
	default:
		if s, err := value.String(); err == nil {
			return patientIDFromReferenceString(s)
		}
	}
	return ""
}

func patientIDFromReference(ref *dtpb.Reference) string {
	if ref == nil {
		return ""
	}
	if ref.GetPatientId() != nil {
		return strings.TrimSpace(ref.GetPatientId().GetValue())
	}
	if ref.GetUri() != nil {
		return patientIDFromReferenceString(ref.GetUri().GetValue())
	}
	if ref.GetResourceId() != nil {
		return patientIDFromReferenceString(ref.GetResourceId().GetValue())
	}
	return ""
}

func patientIDFromReferenceString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "Patient/") {
		return strings.TrimSpace(strings.TrimPrefix(raw, "Patient/"))
	}
	if i := strings.Index(raw, "/"); i > 0 {
		if raw[:i] == "Patient" && i < len(raw)-1 {
			return raw[i+1:]
		}
		return ""
	}
	return ""
}
