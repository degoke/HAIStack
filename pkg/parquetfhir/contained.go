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
		for _, key := range sortedKeys(resource) {
			childValue := resource[key]
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			if key == "resourceType" {
				child.fhirType = "code"
			}
			b.observeValue(child, childValue)
		}
	}
}

func (b *SchemaBuilder) observeValue(field *observedField, value any) {
	switch v := value.(type) {
	case []any:
		field.repeating = true
		for _, item := range v {
			if item == nil {
				continue
			}
			b.observeItem(field, "", item)
		}
	case map[string]any:
		for _, key := range sortedKeys(v) {
			childValue := v[key]
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			b.observeValue(child, childValue)
		}
	}
}
