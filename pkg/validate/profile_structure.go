package validate

import (
	"context"
	"fmt"
	"strings"
)

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

func validateProfileSnapshotStructure(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, issues *[]ValidationIssue) {
	state := newProfileStructureState(sd)
	accumulateProfilePathCounts(obj, sd.Type, state.counts)
	walkProfileStructure(ctx, obj, sd.Type, state, issues)
	validateProfileSlicingWithCounts(ctx, obj, sd, state, issues)
}

func (state *profileStructureState) pathCount(path string) int {
	return state.counts[path]
}

func walkProfileStructure(ctx context.Context, node interface{}, path string, state *profileStructureState, issues *[]ValidationIssue) {
	if err := ctx.Err(); err != nil {
		return
	}
	sd := state.sd
	switch current := node.(type) {
	case map[string]interface{}:
		if isMetaElementPath(path) {
			for key, value := range current {
				if err := ctx.Err(); err != nil {
					return
				}
				if _, ok := metaFieldAllowlist[key]; !ok {
					*issues = append(*issues, issue(
						"unknown-element",
						fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
						[]string{path + "." + key},
					))
					continue
				}
				walkProfileStructure(ctx, value, path+"."+key, state, issues)
			}
			return
		}
		allowed := sd.allowedChild[path]
		for key, value := range current {
			if err := ctx.Err(); err != nil {
				return
			}
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
					walkProfileStructure(ctx, value, nextPath, state, issues)
				}
				continue
			}
			if allowed == nil {
				if hasElementPath(sd, path) || isUnderOpaqueComplexType(sd, path) {
					nextPath := elementPathForJSONKey(sd, path, key)
					walkProfileStructure(ctx, value, nextPath, state, issues)
					continue
				}
				*issues = append(*issues, issue(
					"unknown-element",
					fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
					[]string{path + "." + key},
				))
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
			walkProfileStructure(ctx, value, nextPath, state, issues)
		}
	case []interface{}:
		for _, item := range current {
			walkProfileStructure(ctx, item, path, state, issues)
		}
	}
}

func hasElementPath(sd *StructureDefinition, path string) bool {
	for _, el := range sd.Elements {
		if el.Path == path {
			return true
		}
	}
	return false
}

func isUnderOpaqueComplexType(sd *StructureDefinition, path string) bool {
	best := ""
	for _, el := range sd.Elements {
		if el.Path == "" || el.Path == path {
			continue
		}
		if strings.HasPrefix(path, el.Path+".") && len(el.Path) > len(best) {
			best = el.Path
		}
	}
	if best == "" {
		return false
	}
	children := sd.allowedChild[best]
	return len(children) == 0
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
