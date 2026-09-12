package parquetfhir

import (
	"strings"

	"github.com/parquet-go/parquet-go"
)

// BuildSchema returns a parquet schema for the observed fields.
func (b *SchemaBuilder) BuildSchema() *parquet.Schema {
	group := parquet.Group{
		"resourceType": parquet.Required(parquet.String()),
	}
	for _, name := range sortedKeys(b.root.children) {
		child := b.root.children[name]
		if node := b.buildNode(name, b.index.resourceType+"."+name, child); node != nil {
			group[name] = node
		}
	}
	return parquet.NewSchema(b.index.resourceType, group)
}

func (b *SchemaBuilder) buildNode(fieldName, sdPath string, observed *observedField) parquet.Node {
	if observed == nil {
		return nil
	}
	if observed.fhirType == "decimalAnnotation" {
		return parquet.Optional(decimalAnnotationNode())
	}
	if isAnnotationField(fieldName) {
		if strings.HasSuffix(fieldName, "_numeric") || observed.fhirType == "decimalAnnotation" {
			return parquet.Optional(decimalAnnotationNode())
		}
		if strings.HasSuffix(fieldName, "_start") || strings.HasSuffix(fieldName, "_end") || observed.fhirType == "timestampAnnotation" {
			return parquet.Optional(timestampAnnotationNode())
		}
	}
	if observed.fhirType == "QuantityCanonical" {
		return b.buildQuantityCanonicalNode(observed)
	}

	el := b.index.lookup(sdPath, fieldName)
	repeating := observed.repeating || (el != nil && isRepeatingMax(el.Max))
	fhirType := observed.fhirType
	if fhirType == "" {
		fhirType = elementType(el, fieldName, b.index.choiceTypes)
	}

	if len(observed.children) == 0 {
		node := primitiveNode(fhirType)
		if repeating {
			return parquet.Optional(parquet.List(node))
		}
		return parquet.Optional(node)
	}

	childGroup := parquet.Group{}
	for _, name := range sortedKeys(observed.children) {
		childObs := observed.children[name]
		childPath := sdPath + "." + name
		if node := b.buildNode(name, childPath, childObs); node != nil {
			childGroup[name] = node
		}
	}
	if len(childGroup) == 0 {
		return nil
	}

	inner := parquet.Group(childGroup)
	if repeating {
		return parquet.Optional(parquet.List(inner))
	}
	return parquet.Optional(inner)
}

func (b *SchemaBuilder) buildQuantityCanonicalNode(observed *observedField) parquet.Node {
	group := parquet.Group{}
	for _, name := range sortedKeys(observed.children) {
		child := observed.children[name]
		switch {
		case name == quantityValueNumericField() || child.fhirType == "decimalAnnotation":
			group[name] = parquet.Optional(decimalAnnotationNode())
		case name == "value":
			group[name] = parquet.Optional(parquet.String())
		default:
			group[name] = parquet.Optional(parquet.String())
		}
	}
	return parquet.Optional(group)
}
