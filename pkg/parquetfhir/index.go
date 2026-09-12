package parquetfhir

import (
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/validate"
)

type elementIndex struct {
	resourceType string
	byPath       map[string]*validate.ElementDefinition
	choiceTypes  map[string]string
}

func newElementIndex(sd *validate.StructureDefinition) (*elementIndex, error) {
	if sd == nil || sd.Type == "" {
		return nil, fmt.Errorf("parquetfhir: StructureDefinition resource type is required")
	}
	idx := &elementIndex{
		resourceType: sd.Type,
		byPath:       make(map[string]*validate.ElementDefinition, len(sd.Elements)),
		choiceTypes:  make(map[string]string),
	}
	for i := range sd.Elements {
		el := &sd.Elements[i]
		idx.byPath[el.Path] = el
		if !strings.Contains(el.Path, "[x]") {
			continue
		}
		base := choiceBaseName(el.Path)
		for _, typ := range el.Types {
			name := choiceFieldName(base, typ)
			idx.choiceTypes[name] = normalizeFHIRType(typ)
		}
	}
	return idx, nil
}

func (idx *elementIndex) lookup(path, fieldName string) *validate.ElementDefinition {
	if el, ok := idx.byPath[path]; ok {
		return el
	}
	if el, ok := idx.byPath[path+"[x]"]; ok {
		return el
	}
	return nil
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
	return base + upperFirst(normalizeFHIRType(typ))
}
