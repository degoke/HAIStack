package validate

import (
	"context"
	"fmt"
	"strings"
)

// profileStructureState holds StructureDefinition context for profile structure walks.
type profileStructureState struct {
	sd               *StructureDefinition
	sliceParents     map[string]*ElementSlicing
	elementsByParent map[string][]ElementDefinition
}

func newProfileStructureState(sd *StructureDefinition) *profileStructureState {
	sd.buildAllowedChildren()
	return &profileStructureState{
		sd:           sd,
		sliceParents: buildSliceParents(sd),
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

func validateProfileSnapshotStructure(ctx context.Context, obj map[string]interface{}, sd *StructureDefinition, catalog ProfileCatalog, issues *[]ValidationIssue) {
	state := newProfileStructureState(sd)
	walkOpts := profileWalkOptions{unknownElements: true, cardinality: true, catalog: catalog}
	walkProfileStructureWithOptions(ctx, obj, sd.Type, state, issues, walkOpts)
	validateProfileSliceCardinality(ctx, obj, sd, state, issues)
}

func walkProfileStructure(ctx context.Context, node interface{}, path string, state *profileStructureState, issues *[]ValidationIssue) {
	walkProfileStructureWithOptions(ctx, node, path, state, issues, profileWalkOptions{unknownElements: true, cardinality: true})
}

func walkProfileStructureWithOptions(ctx context.Context, node interface{}, path string, state *profileStructureState, issues *[]ValidationIssue, opts profileWalkOptions) {
	if err := ctx.Err(); err != nil {
		return
	}
	sd := state.sd
	switch current := node.(type) {
	case map[string]interface{}:
		if opts.cardinality {
			checkCardinalityAtParent(ctx, current, path, state, issues)
		}
		if isMetaElementPath(path) {
			for key, value := range current {
				if err := ctx.Err(); err != nil {
					return
				}
				if _, ok := metaFieldAllowlist[key]; !ok {
					if opts.unknownElements {
						*issues = append(*issues, issue(
							"unknown-element",
							fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
							[]string{path + "." + key},
						))
					}
					continue
				}
				walkProfileStructureWithOptions(ctx, value, path+"."+key, state, issues, opts)
			}
			return
		}
		allowed := sd.allowedChild[path]
		for key, value := range current {
			if err := ctx.Err(); err != nil {
				return
			}
			if isAlwaysAllowedKey(key) {
				if key == "contained" {
					walkContainedResources(ctx, value, opts, issues)
					continue
				}
				nextPath := path
				if key == "meta" || key == "text" {
					if path == "" {
						nextPath = key
					} else {
						nextPath = path + "." + key
					}
				}
				if key != "extension" && key != "modifierExtension" {
					walkProfileStructureWithOptions(ctx, value, nextPath, state, issues, opts)
				}
				continue
			}
			if allowed == nil {
				if hasElementPath(sd, path) || isUnderOpaqueComplexType(sd, path) {
					nextPath := elementPathForJSONKey(sd, path, key)
					walkProfileStructureWithOptions(ctx, value, nextPath, state, issues, opts)
					continue
				}
				if opts.unknownElements {
					*issues = append(*issues, issue(
						"unknown-element",
						fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
						[]string{path + "." + key},
					))
				}
				continue
			}
			if _, ok := allowed[key]; !ok {
				if opts.unknownElements {
					*issues = append(*issues, issue(
						"unknown-element",
						fmt.Sprintf("element %q is not allowed at %s (%s)", key, path, sd.URL),
						[]string{path + "." + key},
					))
				}
				continue
			}
			nextPath := elementPathForJSONKey(sd, path, key)
			walkProfileStructureWithOptions(ctx, value, nextPath, state, issues, opts)
		}
	case []interface{}:
		for _, item := range current {
			walkProfileStructureWithOptions(ctx, item, path, state, issues, opts)
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

func walkContainedResources(ctx context.Context, value interface{}, opts profileWalkOptions, issues *[]ValidationIssue) {
	arr, ok := value.([]interface{})
	if !ok {
		return
	}
	for _, item := range arr {
		if err := ctx.Err(); err != nil {
			return
		}
		contained, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		resourceType, _ := contained["resourceType"].(string)
		if resourceType == "" || opts.catalog == nil {
			continue
		}
		containedSD, ok := opts.catalog.GetStructureDefinition(BaseStructureDefinitionURL(resourceType))
		if !ok || containedSD == nil {
			continue
		}
		containedState := newProfileStructureState(containedSD)
		containedOpts := profileWalkOptions{
			unknownElements: opts.unknownElements,
			cardinality:     opts.cardinality,
			catalog:         opts.catalog,
		}
		walkProfileStructureWithOptions(ctx, contained, containedSD.Type, containedState, issues, containedOpts)
		if containedSD.UseSnapshot {
			validateProfileSliceCardinality(ctx, contained, containedSD, containedState, issues)
		}
	}
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
