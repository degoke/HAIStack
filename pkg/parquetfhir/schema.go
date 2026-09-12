package parquetfhir

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/parquet-go/parquet-go"
)

var fhirPrimitiveTypes = map[string]struct{}{
	"base64Binary": {}, "boolean": {}, "canonical": {}, "code": {}, "date": {},
	"dateTime": {}, "decimal": {}, "id": {}, "instant": {}, "integer": {},
	"integer64": {}, "markdown": {}, "oid": {}, "positiveInt": {}, "string": {},
	"time": {}, "unsignedInt": {}, "uri": {}, "url": {}, "uuid": {}, "xhtml": {},
}

type elementIndex struct {
	resourceType string
	byPath       map[string]*validate.ElementDefinition
	choiceFields map[string]*validate.ElementDefinition
}

func newElementIndex(sd *validate.StructureDefinition) (*elementIndex, error) {
	if sd == nil || sd.Type == "" {
		return nil, fmt.Errorf("parquetfhir: StructureDefinition resource type is required")
	}
	idx := &elementIndex{
		resourceType: sd.Type,
		byPath:       make(map[string]*validate.ElementDefinition, len(sd.Elements)),
		choiceFields: make(map[string]*validate.ElementDefinition),
	}
	for i := range sd.Elements {
		el := &sd.Elements[i]
		idx.byPath[el.Path] = el
		if strings.Contains(el.Path, "[x]") {
			base := choiceBaseName(el.Path)
			for _, typ := range el.Types {
				if isPrimitiveFHIRType(typ) {
					idx.choiceFields[choiceFieldName(base, typ)] = el
				}
			}
		}
	}
	return idx, nil
}

type observedField struct {
	repeating bool
	children  map[string]*observedField
}

// SchemaBuilder derives a Parquet-on-FHIR schema from a StructureDefinition and
// the union of fields observed in resource payloads.
type SchemaBuilder struct {
	index *elementIndex
	root  *observedField
}

// NewSchemaBuilder indexes sd for schema generation.
func NewSchemaBuilder(sd *validate.StructureDefinition) (*SchemaBuilder, error) {
	idx, err := newElementIndex(sd)
	if err != nil {
		return nil, err
	}
	return &SchemaBuilder{
		index: idx,
		root:  &observedField{children: make(map[string]*observedField)},
	}, nil
}

// ObserveResource records fields present in one FHIR resource JSON object.
func (b *SchemaBuilder) ObserveResource(raw map[string]any) {
	if b == nil || raw == nil {
		return
	}
	for key, value := range raw {
		if key == "resourceType" || value == nil {
			continue
		}
		field := b.ensureChild(b.root, key)
		path := b.index.resourceType + "." + key
		if el := b.lookupElement(path, key); el != nil && isRepeating(el.Max) {
			field.repeating = true
		}
		b.observe(field, path, key, value)
	}
}

func (b *SchemaBuilder) observe(field *observedField, sdPath, fieldName string, value any) {
	switch v := value.(type) {
	case []any:
		field.repeating = true
		for _, item := range v {
			if item == nil {
				continue
			}
			b.observeItem(field, sdPath, fieldName, item)
		}
	case map[string]any:
		for key, childValue := range v {
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			childPath := sdPath + "." + key
			if el := b.lookupElement(childPath, key); el != nil && isRepeating(el.Max) {
				child.repeating = true
			}
			b.observe(child, childPath, key, childValue)
		}
	}
}

func (b *SchemaBuilder) observeItem(field *observedField, sdPath, fieldName string, item any) {
	switch v := item.(type) {
	case map[string]any:
		for key, childValue := range v {
			if childValue == nil {
				continue
			}
			child := b.ensureChild(field, key)
			childPath := sdPath + "." + key
			if el := b.lookupElement(childPath, key); el != nil && isRepeating(el.Max) {
				child.repeating = true
			}
			b.observe(child, childPath, key, childValue)
		}
	default:
		_ = v
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

func (b *SchemaBuilder) lookupElement(path, fieldName string) *validate.ElementDefinition {
	if el, ok := b.index.byPath[path]; ok {
		return el
	}
	if el, ok := b.index.choiceFields[fieldName]; ok {
		return el
	}
	return nil
}

// BuildSchema returns a parquet schema for the observed fields.
func (b *SchemaBuilder) BuildSchema() *parquet.Schema {
	group := parquet.Group{
		"resourceType": parquet.Required(parquet.String()),
	}
	for name, child := range b.root.children {
		node := b.buildNode(name, b.index.resourceType+"."+name, child)
		if node != nil {
			group[name] = node
		}
	}
	return parquet.NewSchema(b.index.resourceType, group)
}

func (b *SchemaBuilder) buildNode(fieldName, sdPath string, observed *observedField) parquet.Node {
	el := b.lookupElement(sdPath, fieldName)
	repeating := observed.repeating || (el != nil && isRepeating(el.Max))

	if len(observed.children) == 0 {
		node := primitiveNode(elementType(el, fieldName))
		if repeating {
			node = parquet.Optional(parquet.List(node))
			return node
		}
		return parquet.Optional(node)
	}

	childGroup := parquet.Group{}
	for name, childObs := range observed.children {
		childPath := sdPath + "." + name
		if node := b.buildNode(name, childPath, childObs); node != nil {
			childGroup[name] = node
		}
	}
	if len(childGroup) == 0 {
		return nil
	}
	node := parquet.Optional(childGroup)
	if repeating {
		node = parquet.Optional(parquet.List(childGroup))
	}
	return node
}

func elementType(el *validate.ElementDefinition, fieldName string) string {
	if el == nil {
		return "string"
	}
	if strings.Contains(el.Path, "[x]") {
		for _, typ := range el.Types {
			if choiceFieldName(choiceBaseName(el.Path), typ) == fieldName {
				return typ
			}
		}
	}
	if len(el.Types) == 1 {
		return el.Types[0]
	}
	for _, typ := range el.Types {
		if isPrimitiveFHIRType(typ) {
			return typ
		}
	}
	if len(el.Types) > 0 {
		return el.Types[0]
	}
	return "string"
}

func primitiveNode(fhirType string) parquet.Node {
	switch fhirType {
	case "boolean":
		return parquet.Leaf(parquet.BooleanType)
	case "integer":
		return parquet.Int(32)
	case "integer64":
		return parquet.Int(64)
	case "positiveInt", "unsignedInt":
		return parquet.Int(32)
	default:
		return parquet.String()
	}
}

func isPrimitiveFHIRType(typ string) bool {
	if typ == "" {
		return false
	}
	if strings.HasPrefix(typ, "http://") {
		return true
	}
	_, ok := fhirPrimitiveTypes[typ]
	return ok
}

func isRepeating(max string) bool {
	if max == "" || max == "1" {
		return false
	}
	if max == "*" {
		return true
	}
	n, err := strconv.Atoi(max)
	return err == nil && n > 1
}

func choiceBaseName(path string) string {
	idx := strings.LastIndex(path, ".")
	base := path
	if idx >= 0 {
		base = path[idx+1:]
	}
	return strings.TrimSuffix(base, "[x]")
}

func choiceFieldName(base, typ string) string {
	if typ == "" {
		return base
	}
	if strings.HasPrefix(typ, "http://") {
		typ = "string"
	}
	return base + upperFirst(typ)
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
