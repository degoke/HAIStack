package sdc

import (
	"context"
	"encoding/json"
	"strings"
)

// ResolvedElement carries StructureDefinition element metadata for assembly.
type ResolvedElement struct {
	Text      string
	Short     string
	Min       *int
	MaxLength *int
}

// DefinitionElementResolver resolves item.definition element metadata during assembly.
type DefinitionElementResolver interface {
	ResolveElement(ctx context.Context, definition string) (ResolvedElement, error)
}

// StoreDefinitionElementResolver resolves elements from store.DefinitionStore StructureDefinitions.
type StoreDefinitionElementResolver struct {
	Store DefinitionStore
}

func (r StoreDefinitionElementResolver) ResolveElement(ctx context.Context, definition string) (ResolvedElement, error) {
	if r.Store == nil {
		return ResolvedElement{}, context.Canceled
	}
	hash := strings.Index(definition, "#")
	if hash < 0 {
		return ResolvedElement{}, context.Canceled
	}
	canonical := definition[:hash]
	elementID := definition[hash+1:]
	version := ""
	if bar := strings.Index(canonical, "|"); bar >= 0 {
		version = canonical[bar+1:]
		canonical = canonical[:bar]
	}
	record, err := r.Store.Get(ctx, canonical, version)
	if err != nil || record == nil {
		return ResolvedElement{}, err
	}
	var sd map[string]any
	if err := json.Unmarshal(record.JSONData, &sd); err != nil {
		return ResolvedElement{}, err
	}
	element := findStructureDefinitionElement(sd, elementID)
	if element == nil {
		return ResolvedElement{}, context.Canceled
	}
	return elementSnapshot(element), nil
}

func findStructureDefinitionElement(sd map[string]any, elementID string) map[string]any {
	for _, key := range []string{"snapshot", "differential"} {
		section, _ := sd[key].(map[string]any)
		if section == nil {
			continue
		}
		elements, _ := section["element"].([]any)
		if found := findElementByID(elements, elementID); found != nil {
			return found
		}
	}
	return nil
}

func findElementByID(elements []any, elementID string) map[string]any {
	for _, raw := range elements {
		element, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := element["id"].(string); id == elementID {
			return element
		}
		if children, ok := element["element"].([]any); ok {
			if found := findElementByID(children, elementID); found != nil {
				return found
			}
		}
	}
	return nil
}

func elementSnapshot(element map[string]any) ResolvedElement {
	out := ResolvedElement{}
	if text, ok := element["definition"].(string); ok {
		out.Text = text
	}
	if short, ok := element["short"].(string); ok {
		out.Short = short
	}
	if min, ok := element["min"].(float64); ok {
		v := int(min)
		out.Min = &v
	}
	if maxLength, ok := element["maxLength"].(float64); ok {
		v := int(maxLength)
		out.MaxLength = &v
	}
	return out
}

func isElementDefinition(definition string) bool {
	return strings.Contains(definition, "#")
}

func questionnaireModuleCanonical(it Item) string {
	if it.SubQuestionnaire != "" {
		return it.SubQuestionnaire
	}
	if it.Definition != "" && !isElementDefinition(it.Definition) {
		return it.Definition
	}
	return ""
}

func propagateDefinitionMetadata(ctx context.Context, item *Item, resolver DefinitionElementResolver, o *Outcome) {
	if resolver == nil || item == nil || !isElementDefinition(item.Definition) {
		return
	}
	element, err := resolver.ResolveElement(ctx, item.Definition)
	if err != nil {
		o.add("warning", "not-found", "could not resolve item definition "+item.Definition, "Questionnaire.item["+item.LinkID+"]")
		return
	}
	if item.Text == "" {
		item.Text = firstNonEmpty(element.Text, element.Short)
	}
	if element.Min != nil && *element.Min > 0 {
		item.Required = true
	}
	if item.MaxLength == nil && element.MaxLength != nil {
		item.MaxLength = element.MaxLength
	}
}

func mergeContainedResources(dst *Questionnaire, src Questionnaire, prefix string) {
	for _, resource := range src.Contained {
		cloned := cloneMap(resource)
		if cloned == nil {
			continue
		}
		if id, ok := cloned["id"].(string); ok && id != "" {
			cloned["id"] = prefix + id
		}
		dst.Contained = append(dst.Contained, cloned)
	}
}
