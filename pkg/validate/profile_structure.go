package validate

import "fmt"

// profileStructureState accumulates path counts and slice metadata during a single
// resource tree walk used for cardinality and unknown-element checks.
type profileStructureState struct {
	sd           *StructureDefinition
	sliceParents map[string]*ElementSlicing
	counts       map[string]int
}

func newProfileStructureState(sd *StructureDefinition) *profileStructureState {
	sd.buildAllowedChildren()
	return &profileStructureState{
		sd:           sd,
		sliceParents: buildSliceParents(sd),
		counts:       make(map[string]int),
	}
}

func buildSliceParents(sd *StructureDefinition) map[string]*ElementSlicing {
	sliceParents := make(map[string]*ElementSlicing)
	for i := range sd.Elements {
		el := &sd.Elements[i]
		if el.Slicing != nil {
			sliceParents[el.Path] = el.Slicing
		}
	}
	return sliceParents
}

func validateProfileSnapshotStructure(obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	state := newProfileStructureState(sd)
	accumulateProfilePathCounts(obj, sd.Type, state.counts)
	walkProfileStructure(obj, sd.Type, state, issues)
	validateProfileSlicingWithCounts(obj, sd, state, issues)
}

func (state *profileStructureState) pathCount(path string) int {
	return state.counts[path]
}

func walkProfileStructure(node interface{}, path string, state *profileStructureState, issues *[]ValidationIssue) {
	sd := state.sd
	switch current := node.(type) {
	case map[string]interface{}:
		if isMetaElementPath(path) {
			for key, value := range current {
				if _, ok := metaFieldAllowlist[key]; !ok {
					*issues = append(*issues, issue(
						"unknown-element",
						fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
						[]string{path + "." + key},
					))
					continue
				}
				walkProfileStructure(value, path+"."+key, state, issues)
			}
			return
		}
		allowed := sd.allowedChild[path]
		for key, value := range current {
			if isAlwaysAllowedKey(key) {
				nextPath := path
				if key == "meta" || key == "text" {
					if path == "" {
						nextPath = key
					} else {
						nextPath = path + "." + key
					}
				}
				if key != "extension" && key != "modifierExtension" {
					walkProfileStructure(value, nextPath, state, issues)
				}
				continue
			}
			if allowed == nil {
				continue
			}
			if _, ok := allowed[key]; !ok {
				*issues = append(*issues, issue(
					"unknown-element",
					fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
					[]string{path + "." + key},
				))
				continue
			}
			nextPath := elementPathForJSONKey(sd, path, key)
			walkProfileStructure(value, nextPath, state, issues)
		}
	case []interface{}:
		for _, item := range current {
			walkProfileStructure(item, path, state, issues)
		}
	}
}

func accumulateProfilePathCounts(node interface{}, path string, counts map[string]int) {
	switch current := node.(type) {
	case []interface{}:
		counts[path] += len(current)
		for _, item := range current {
			accumulateProfilePathCounts(item, path, counts)
		}
	case map[string]interface{}:
		if path != "" {
			counts[path]++
		}
		for key, value := range current {
			childPath := path + "." + key
			accumulateProfilePathCounts(value, childPath, counts)
		}
	default:
		if path != "" {
			counts[path]++
		}
	}
}
