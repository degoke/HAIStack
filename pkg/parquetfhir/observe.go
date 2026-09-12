package parquetfhir

import (
	"github.com/degoke/health-ai-stack/pkg/validate"
)

type observedField struct {
	repeating bool
	children  map[string]*observedField
	fhirType  string
}

// SchemaBuilder derives a Parquet-on-FHIR schema from a StructureDefinition and
// the union of fields present in exported resource payloads.
type SchemaBuilder struct {
	index   *elementIndex
	root    *observedField
	catalog validate.ProfileCatalog
}

// NewSchemaBuilder indexes sd for schema generation.
func NewSchemaBuilder(sd *validate.StructureDefinition, catalog validate.ProfileCatalog) (*SchemaBuilder, error) {
	idx, err := newElementIndex(sd)
	if err != nil {
		return nil, err
	}
	return &SchemaBuilder{
		index:   idx,
		root:    &observedField{children: make(map[string]*observedField)},
		catalog: catalog,
	}, nil
}

// Index returns the merged element index used for annotation enrichment.
func (b *SchemaBuilder) Index() *elementIndex {
	if b == nil {
		return nil
	}
	return b.index
}

// ObserveResource records fields present in one FHIR resource JSON object.
func (b *SchemaBuilder) ObserveResource(raw map[string]any) {
	if b == nil || raw == nil {
		return
	}
	mergeResourceProfiles(b.index, b.catalog, raw)
	for _, key := range sortedKeys(raw) {
		value := raw[key]
		if key == "resourceType" || value == nil || isAnnotationField(key) {
			continue
		}
		field := b.ensureChild(b.root, key)
		path := b.index.resourceType + "." + key
		el := b.index.lookup(path, key)
		if typ := b.index.resolveFieldType(path, key, ""); typ != "" {
			field.fhirType = typ
		} else if typ := elementType(el, key, b.index.choiceTypes); typ != "" {
			field.fhirType = typ
		}
		if el != nil && isRepeatingMax(el.Max) {
			field.repeating = true
		}
		if key == "contained" {
			field.fhirType = "Resource"
			field.repeating = true
			b.observeContained(field, value)
		} else {
			b.observe(field, path, field.fhirType, value)
		}
		b.observeAnnotations(b.root, field, key)
	}
}

func (b *SchemaBuilder) observeAnnotations(parent, field *observedField, fieldName string) {
	if isDateLikeType(field.fhirType) {
		start := b.ensureChild(parent, annotationStartField(fieldName))
		start.fhirType = "timestampAnnotation"
		end := b.ensureChild(parent, annotationEndField(fieldName))
		end.fhirType = "timestampAnnotation"
	}
	if isDecimalType(field.fhirType) {
		num := b.ensureChild(parent, annotationNumericField(fieldName))
		num.fhirType = "decimalAnnotation"
	}
	if field.fhirType == "Quantity" {
		canonical := b.ensureChild(parent, annotationCanonicalField(fieldName))
		canonical.fhirType = "QuantityCanonical"
		b.ensureChild(canonical, "value").fhirType = "decimal"
		b.ensureChild(canonical, "code")
		b.ensureChild(canonical, "system")
		b.ensureChild(canonical, "unit")
		num := b.ensureChild(canonical, quantityValueNumericField())
		num.fhirType = "decimalAnnotation"
	}
}

func (b *SchemaBuilder) observe(field *observedField, sdPath, parentType string, value any) {
	switch v := value.(type) {
	case []any:
		field.repeating = true
		for _, item := range v {
			if item == nil {
				continue
			}
			b.observeItem(field, sdPath, parentType, item)
		}
	case map[string]any:
		for _, key := range sortedKeys(v) {
			childValue := v[key]
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			childPath := sdPath + "." + key
			el := b.index.lookup(childPath, key)
			if typ := b.index.resolveFieldType(childPath, key, parentType); typ != "" {
				child.fhirType = typ
			} else if typ := elementType(el, key, b.index.choiceTypes); typ != "" {
				child.fhirType = typ
			}
			if el != nil && isRepeatingMax(el.Max) {
				child.repeating = true
			}
			if key == "value" && field.fhirType == "Quantity" {
				child.fhirType = "decimal"
				num := b.ensureChild(field, quantityValueNumericField())
				num.fhirType = "decimalAnnotation"
			}
			nestedParent := parentType
			if nested := nestedParentType(el, child.fhirType); nested != "" {
				nestedParent = nested
			}
			b.observe(child, childPath, nestedParent, childValue)
			b.observeAnnotations(field, child, key)
		}
	}
}

func (b *SchemaBuilder) observeItem(field *observedField, sdPath, parentType string, item any) {
	switch v := item.(type) {
	case map[string]any:
		for _, key := range sortedKeys(v) {
			childValue := v[key]
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			childPath := sdPath + "." + key
			el := b.index.lookup(childPath, key)
			if typ := b.index.resolveFieldType(childPath, key, parentType); typ != "" {
				child.fhirType = typ
			} else if typ := elementType(el, key, b.index.choiceTypes); typ != "" {
				child.fhirType = typ
			}
			if el != nil && isRepeatingMax(el.Max) {
				child.repeating = true
			}
			nestedParent := parentType
			if nested := nestedParentType(el, child.fhirType); nested != "" {
				nestedParent = nested
			}
			b.observe(child, childPath, nestedParent, childValue)
			b.observeAnnotations(field, child, key)
		}
	}
}

func (b *SchemaBuilder) ensureChild(parent *observedField, name string) *observedField {
	if parent.children == nil {
		parent.children = make(map[string]*observedField)
	}
	child, ok := parent.children[name]
	if !ok {
		child = &observedField{children: make(map[string]*observedField)}
		parent.children[name] = child
	}
	return child
}
