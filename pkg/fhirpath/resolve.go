package fhirpath

import (
	"context"
	"fmt"
	"strings"

	dtpb "github.com/google/fhir/go/proto/google/fhir/proto/r4/core/datatypes_go_proto"
)

// ResourceResolverConfig configures reference resolution for resolve() and joins.
type ResourceResolverConfig struct {
	Read             func(ctx context.Context, resourceType, id string) (any, error)
	BaseURL          string
	ResolveLogicalID func(ctx context.Context, logicalID string) (resourceType, id string, ok bool)
}

// ResourceStoreResolver returns a ResolveFunc backed by store.ResourceStore.Read.
// It resolves typed relative references, absolute REST URLs, and urn:uuid ids.
func ResourceStoreResolver(read func(ctx context.Context, resourceType, id string) (any, error)) ResolveFunc {
	return EnhancedResourceStoreResolver(ResourceResolverConfig{Read: read})
}

// EnhancedResourceStoreResolver resolves references using ResourceResolverConfig.
func EnhancedResourceStoreResolver(cfg ResourceResolverConfig) ResolveFunc {
	return func(ctx context.Context, ref string) (any, error) {
		if cfg.Read == nil {
			return nil, nil
		}
		resourceType, id, ok := ParseReferenceForRead(ref, cfg.BaseURL)
		if !ok {
			return nil, nil
		}
		if resourceType == "" {
			if cfg.ResolveLogicalID != nil {
				resolvedType, resolvedID, found := cfg.ResolveLogicalID(ctx, id)
				if found {
					resourceType, id = resolvedType, resolvedID
				}
			}
			if resourceType == "" {
				return nil, nil
			}
		}
		return cfg.Read(ctx, resourceType, id)
	}
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
