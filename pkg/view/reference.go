package view

import (
	"context"
	"fmt"
	"strings"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
)

func referenceStringFromValue(v fhirpath.Value) (string, bool) {
	switch val := v.Raw().(type) {
	case *dtpb.Reference:
		return referenceStringFromProto(val)
	default:
		s, err := v.String()
		if err != nil || strings.TrimSpace(s) == "" {
			return "", false
		}
		return strings.TrimSpace(s), true
	}
}

func referenceStringFromProto(ref *dtpb.Reference) (string, bool) {
	if ref == nil {
		return "", false
	}
	if uri := ref.GetUri(); uri != nil && uri.GetValue() != "" {
		return strings.TrimSpace(uri.GetValue()), true
	}
	type idPair struct {
		resourceType string
		id           string
	}
	var pair idPair
	switch {
	case ref.GetPatientId() != nil:
		pair = idPair{"Patient", ref.GetPatientId().GetValue()}
	case ref.GetPractitionerId() != nil:
		pair = idPair{"Practitioner", ref.GetPractitionerId().GetValue()}
	case ref.GetRelatedPersonId() != nil:
		pair = idPair{"RelatedPerson", ref.GetRelatedPersonId().GetValue()}
	case ref.GetOrganizationId() != nil:
		pair = idPair{"Organization", ref.GetOrganizationId().GetValue()}
	case ref.GetEncounterId() != nil:
		pair = idPair{"Encounter", ref.GetEncounterId().GetValue()}
	case ref.GetAppointmentId() != nil:
		pair = idPair{"Appointment", ref.GetAppointmentId().GetValue()}
	case ref.GetObservationId() != nil:
		pair = idPair{"Observation", ref.GetObservationId().GetValue()}
	case ref.GetLocationId() != nil:
		pair = idPair{"Location", ref.GetLocationId().GetValue()}
	case ref.GetDeviceId() != nil:
		pair = idPair{"Device", ref.GetDeviceId().GetValue()}
	case ref.GetMedicationId() != nil:
		pair = idPair{"Medication", ref.GetMedicationId().GetValue()}
	case ref.GetResourceId() != nil:
		pair = idPair{"", ref.GetResourceId().GetValue()}
	}
	if pair.id == "" {
		return "", false
	}
	if pair.resourceType != "" {
		return pair.resourceType + "/" + pair.id, true
	}
	return pair.id, true
}

func parseTypedReference(raw string) (resourceType, id string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") ||
		strings.HasPrefix(raw, "urn:") || strings.HasPrefix(raw, "#") {
		return "", "", false
	}
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[0], ":") {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func (e *Executor) resolveIterationContext(ctx context.Context, item fhirpath.Value) (any, error) {
	raw := item.Raw()
	if raw == nil {
		return nil, nil
	}
	if ref, ok := raw.(*dtpb.Reference); ok {
		return e.resolveReferenceProto(ctx, ref)
	}
	if refStr, ok := referenceStringFromValue(item); ok {
		if resourceType, id, typed := parseTypedReference(refStr); typed {
			env, err := e.cfg.Resources.Read(ctx, resourceType, id)
			if err != nil {
				return nil, fmt.Errorf("resolve reference %q: %w", refStr, err)
			}
			return env, nil
		}
	}
	return raw, nil
}

func (e *Executor) resolveReferenceProto(ctx context.Context, ref *dtpb.Reference) (any, error) {
	refStr, ok := referenceStringFromProto(ref)
	if !ok {
		return ref, nil
	}
	resourceType, id, typed := parseTypedReference(refStr)
	if !typed {
		return ref, nil
	}
	env, err := e.cfg.Resources.Read(ctx, resourceType, id)
	if err != nil {
		return nil, err
	}
	return env, nil
}
