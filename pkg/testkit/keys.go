package testkit

// ResourceKey returns the canonical map key for a FHIR resource type and id.
func ResourceKey(resourceType, id string) string {
	return resourceType + "/" + id
}
