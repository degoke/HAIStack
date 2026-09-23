package ai

// buildEvalPathIndex derives JSON segment paths from compiled FHIRPath expression
// labels. Eval results are not matched by string value (that collides on common
// tokens); only paths implied by the expression text are used.
func buildEvalPathIndex(labels []string) *pathIndex {
	if len(labels) == 0 {
		return nil
	}
	var paths []string
	for _, label := range labels {
		seg := SegmentPathFromFHIRPathExpr(label)
		if seg == "" {
			continue
		}
		paths = append(paths, seg)
	}
	return newPathIndex(paths)
}
