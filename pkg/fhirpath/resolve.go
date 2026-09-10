package fhirpath

import (
	"context"
	"fmt"
	"strings"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
)

// ResourceStoreResolver returns a ResolveFunc backed by store.ResourceStore.Read.
// It resolves typed relative references such as Patient/123.
func ResourceStoreResolver(read func(ctx context.Context, resourceType, id string) (any, error)) ResolveFunc {
	return func(ctx context.Context, ref string) (any, error) {
		resourceType, id, ok := parseTypedReference(ref)
		if !ok {
			return nil, nil
		}
		return read(ctx, resourceType, id)
	}
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

// TerminologyServiceAdapter adapts a validate-code callback to TerminologyValidator.
func TerminologyServiceAdapter(validate func(ctx context.Context, valueSetURL, system, code string) (bool, error)) TerminologyValidator {
	if validate == nil {
		return nil
	}
	return terminologyFunc(validate)
}

type terminologyFunc func(ctx context.Context, valueSetURL, system, code string) (bool, error)

func (f terminologyFunc) MemberOf(ctx context.Context, valueSetURL, system, code string) (bool, error) {
	return f(ctx, valueSetURL, system, code)
}

// ReferenceStringFromProto extracts a reference string from a Google FHIR Reference proto.
func ReferenceStringFromProto(ref *dtpb.Reference) (string, bool) {
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
		return fmt.Sprintf("%s/%s", pair.resourceType, pair.id), true
	}
	return pair.id, true
}
