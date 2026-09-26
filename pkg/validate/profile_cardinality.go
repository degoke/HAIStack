package validate

import (
	"context"
	"strings"
)

type profileWalkOptions struct {
	unknownElements bool
	cardinality     bool
	catalog         ProfileCatalog
}

func buildElementCardinalityIndex(sd *StructureDefinition, sliceParents map[string]*ElementSlicing) map[string][]ElementDefinition {
	index := make(map[string][]ElementDefinition)
	for _, el := range sd.Elements {
		if el.Path == "" || el.Path == sd.Type {
			continue
		}
		if el.SliceName != "" {
			continue
		}
		if strings.Contains(el.Path, ":") {
			continue
		}
		if _, sliced := sliceParents[el.Path]; sliced {
			continue
		}
		parent, child := splitElementPath(el.Path)
		if parent == "" || child == "" {
			continue
		}
		index[parent] = append(index[parent], el)
	}
	return index
}

func (state *profileStructureState) ensureCardinalityIndex() {
	if state.elementsByParent != nil {
		return
	}
	state.elementsByParent = buildElementCardinalityIndex(state.sd, state.sliceParents)
}

// checkCardinalityAtParent enforces SD min/max for direct children of the current
// backbone instance at path (FHIR: cardinality is per parent instance).
func checkCardinalityAtParent(ctx context.Context, current map[string]interface{}, path string, state *profileStructureState, issues *[]ValidationIssue) {
	if err := ctx.Err(); err != nil {
		return
	}
	if current == nil || path == "" {
		return
	}
	state.ensureCardinalityIndex()
	elements := state.elementsByParent[path]
	if len(elements) == 0 {
		return
	}
	profileURL := state.sd.URL
	for _, el := range elements {
		count := countChildInInstance(current, el)
		reportSliceCardinality(el, count, count, profileURL, issues)
	}
}

func walkProfileElementCardinality(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, catalog ProfileCatalog, issues *[]ValidationIssue) {
	state := newProfileStructureState(sd)
	walkProfileStructureWithOptions(ctx, obj, sd.Type, state, issues, profileWalkOptions{cardinality: true, catalog: catalog})
}

func countChildInInstance(instance interface{}, el ElementDefinition) int {
	_, child := splitElementPath(el.Path)
	m, ok := instance.(map[string]interface{})
	if !ok {
		return 0
	}
	if strings.Contains(child, "[x]") {
		total := 0
		for _, key := range choiceJSONKeys(child, el.Types) {
			v, present := m[key]
			if !present || v == nil {
				continue
			}
			if arr, ok := v.([]interface{}); ok {
				total += len(arr)
			} else {
				total++
			}
		}
		return total
	}
	v, present := m[child]
	if !present || v == nil {
		return 0
	}
	if arr, ok := v.([]interface{}); ok {
		return len(arr)
	}
	return 1
}
