package conflict

import (
	"encoding/json"
	"reflect"
	"sort"
)

// ChangeKind describes the nature of a change at a FHIR element path.
type ChangeKind string

const (
	ChangeKindScalarReplace ChangeKind = "scalar_replace"
	ChangeKindArrayAppend   ChangeKind = "array_append"
	ChangeKindArrayRemove   ChangeKind = "array_remove"
	ChangeKindStructural    ChangeKind = "structural"
	ChangeKindUnsupported   ChangeKind = "unsupported"
)

// PathChange records one detected change between two resource snapshots.
type PathChange struct {
	Path     []string
	Kind     ChangeKind
	Base     any
	Value    any
	Appended []any
	Removed  []any
}

// Dotted returns the FHIR-style dotted path including the resource type.
func (c PathChange) Dotted(resourceType string) string {
	return dottedPath(resourceType, c.Path)
}

func diffJSON(base, target []byte) ([]PathChange, error) {
	var b, t any
	if len(base) > 0 {
		if err := json.Unmarshal(base, &b); err != nil {
			return nil, err
		}
	}
	if len(target) > 0 {
		if err := json.Unmarshal(target, &t); err != nil {
			return nil, err
		}
	}
	return diffValue(b, t, nil), nil
}

func diffValue(base, target any, path []string) []PathChange {
	if jsonEqual(base, target) {
		return nil
	}
	if base == nil && target == nil {
		return nil
	}

	bm, bMap := base.(map[string]any)
	tm, tMap := target.(map[string]any)
	if bMap && tMap {
		return diffMaps(bm, tm, path)
	}

	ba, bArr := base.([]any)
	ta, tArr := target.([]any)
	if bArr && tArr {
		return diffArrays(ba, ta, path)
	}

	return []PathChange{{
		Path:  pathCopy(path),
		Kind:  ChangeKindScalarReplace,
		Base:  base,
		Value: target,
	}}
}

func diffMaps(base, target map[string]any, path []string) []PathChange {
	keys := make([]string, 0, len(base)+len(target))
	for k := range base {
		keys = append(keys, k)
	}
	for k := range target {
		if _, ok := base[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var changes []PathChange
	for _, k := range keys {
		if len(path) == 0 && ignoredTopLevel(k) {
			continue
		}
		child := append(pathCopy(path), k)
		bv, bok := base[k]
		tv, tok := target[k]
		if !bok {
			changes = append(changes, addedChange(child, tv))
		} else if !tok {
			changes = append(changes, PathChange{Path: child, Kind: ChangeKindUnsupported, Base: bv})
		} else {
			changes = append(changes, diffValue(bv, tv, child)...)
		}
	}
	return changes
}

func addedChange(path []string, val any) PathChange {
	switch v := val.(type) {
	case []any:
		return PathChange{Path: path, Kind: ChangeKindArrayAppend, Appended: v, Value: val}
	case map[string]any:
		return PathChange{Path: path, Kind: ChangeKindStructural, Value: val}
	default:
		return PathChange{Path: path, Kind: ChangeKindScalarReplace, Value: val}
	}
}

func diffArrays(base, target []any, path []string) []PathChange {
	common := 0
	for common < len(base) && common < len(target) && jsonEqual(base[common], target[common]) {
		common++
	}
	if common == len(base) && len(target) > len(base) {
		return []PathChange{{
			Path:     pathCopy(path),
			Kind:     ChangeKindArrayAppend,
			Appended: target[common:],
			Value:    target,
			Base:     base,
		}}
	}
	if common == len(target) && len(base) > len(target) {
		return []PathChange{{
			Path:    pathCopy(path),
			Kind:    ChangeKindArrayRemove,
			Removed: base[common:],
			Base:    base,
			Value:   target,
		}}
	}
	return []PathChange{{
		Path:  pathCopy(path),
		Kind:  ChangeKindStructural,
		Base:  base,
		Value: target,
	}}
}

func jsonEqual(a, b any) bool {
	return reflect.DeepEqual(a, b)
}

func ignoredTopLevel(k string) bool {
	return k == "id" || k == "meta" || k == "resourceType"
}

func pathCopy(p []string) []string {
	out := make([]string, len(p))
	copy(out, p)
	return out
}
