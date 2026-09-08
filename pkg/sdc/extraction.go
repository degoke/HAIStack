package sdc

import (
	"context"
	"fmt"
	"strings"
)

// QuestionnaireExtractor routes extraction using questionnaire tier-5 metadata.
// SourceStructureMap selects the StructureMap runtime; item definitions and
// ObservationExtract drive definition-based extraction when no map is configured.
type QuestionnaireExtractor struct {
	Definition   DefinitionExtractor
	StructureMap StructureMapExtractor
}

func (e QuestionnaireExtractor) Extract(ctx context.Context, q Questionnaire, r QuestionnaireResponse) (ExtractionResult, error) {
	if q.SourceStructureMap != "" {
		if e.StructureMap.Run == nil {
			return ExtractionResult{}, fmt.Errorf("StructureMap runtime is unavailable for %s", q.SourceStructureMap)
		}
		result, err := e.StructureMap.Extract(ctx, q, r)
		if err != nil {
			return result, err
		}
		result.Diagnostics = append(result.Diagnostics, extractionDiagnostics(q)...)
		return result, nil
	}
	def := e.Definition
	mappings := append([]DefinitionMap(nil), def.Mappings...)
	mappings = append(mappings, definitionMapsFromQuestionnaire(q)...)
	if len(mappings) == 0 {
		return ExtractionResult{}, fmt.Errorf("no extraction mappings available")
	}
	def.Mappings = mappings
	result, err := def.Extract(ctx, q, r)
	if err != nil {
		return result, err
	}
	result.Diagnostics = append(result.Diagnostics, extractionDiagnostics(q)...)
	return result, nil
}

func extractionDiagnostics(q Questionnaire) []ExtractionDiagnostic {
	var out []ExtractionDiagnostic
	if q.SourceStructureMap != "" {
		out = append(out, ExtractionDiagnostic{Severity: "information", Message: "extracted using sourceStructureMap " + q.SourceStructureMap})
	}
	if q.ObservationExtract {
		out = append(out, ExtractionDiagnostic{Severity: "information", Message: "questionnaire observationExtract is enabled"})
	}
	for _, def := range q.AdditionalDefinitions {
		out = append(out, ExtractionDiagnostic{Severity: "information", Message: "additional definition " + def})
	}
	return out
}

func definitionMapsFromQuestionnaire(q Questionnaire) []DefinitionMap {
	var maps []DefinitionMap
	var walk func([]Item)
	walk = func(items []Item) {
		for _, item := range items {
			if item.Definition != "" {
				if m, ok := definitionMapFromItem(item, q.ObservationExtract); ok {
					maps = append(maps, m)
				}
			}
			walk(item.Item)
		}
	}
	walk(q.Item)
	return maps
}

func definitionMapFromItem(item Item, observationExtract bool) (DefinitionMap, bool) {
	resourceType, path, ok := parseCanonicalElementPath(item.Definition)
	if !ok {
		return DefinitionMap{}, false
	}
	if observationExtract && !strings.EqualFold(resourceType, "Observation") {
		return DefinitionMap{}, false
	}
	return DefinitionMap{LinkID: item.LinkID, ResourceType: resourceType, Path: path}, true
}

func parseCanonicalElementPath(definition string) (resourceType, path string, ok bool) {
	hash := strings.Index(definition, "#")
	if hash < 0 {
		return "", "", false
	}
	elementPath := definition[hash+1:]
	dot := strings.Index(elementPath, ".")
	if dot < 0 {
		return "", "", false
	}
	resourceType = elementPath[:dot]
	path = elementPath[dot+1:]
	if resourceType == "" || path == "" {
		return "", "", false
	}
	return resourceType, path, true
}
