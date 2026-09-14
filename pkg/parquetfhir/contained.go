package parquetfhir

func (b *SchemaBuilder) observeContained(field *observedField, value any) {
	items, ok := value.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		resource, ok := item.(map[string]any)
		if !ok || resource == nil {
			continue
		}
		resourceType := stringOrEmpty(resource["resourceType"])
		for _, key := range sortedKeys(resource) {
			childValue := resource[key]
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			childPath := resourceType + "." + key
			if key == "resourceType" {
				child.fhirType = "code"
			} else if typ := b.index.resolveFieldType(childPath, key, resourceType); typ != "" {
				child.fhirType = typ
			}
			nestedParent := resourceType
			if nested := nestedParentType(nil, child.fhirType); nested != "" {
				nestedParent = nested
			}
			b.observe(child, childPath, nestedParent, childValue)
			b.observeAnnotations(field, child, key)
		}
	}
}
