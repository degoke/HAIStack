package storetest

import "github.com/degoke/health-ai-stack/pkg/testkit"

// ResourceKey returns the canonical map key for a FHIR resource type and id.
//
// Deprecated: use testkit.ResourceKey. This compatibility wrapper keeps older
// storetest consumers source-compatible while maintaining one implementation.
func ResourceKey(resourceType, id string) string {
	return testkit.ResourceKey(resourceType, id)
}
