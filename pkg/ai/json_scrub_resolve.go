package ai

import "context"

func (d *FHIRDeidentifier) jsonScrubResolve(ctx context.Context, toolName string) jsonScrubResolve {
	catalog := d.catalog
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	return func(resourceType string, resourceJSON []byte) (*pathIndex, *pathIndex, bool, error) {
		if resourceType == "" {
			resourceType = resourceTypeFromJSON(resourceJSON, "")
		}
		bundle, err := d.index.bundleFor(ctx, resourceType, resourceMapForProfileIndex(resourceJSON))
		if err != nil {
			return nil, nil, false, err
		}
		segmentIdx := bundle.segmentIdx
		evalMode := effectiveEvalMode(d.evalMode, toolName)
		if evalMode != EvalModeNever && len(bundle.labels) > 0 {
			segmentIdx = mergePathIndices(segmentIdx, buildEvalPathIndex(bundle.labels))
		}
		strict := strictRedactionFromJSON(resourceJSON, catalog)
		catIdx := bundle.catalogIdx
		if catIdx == nil {
			catIdx = catalogPathIndex(catalog, resourceType)
		}
		if segmentIdx == nil {
			segmentIdx = catIdx
		}
		return segmentIdx, catIdx, strict, nil
	}
}
