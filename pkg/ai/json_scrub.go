package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/buger/jsonparser"
)

type jsonScrubber struct {
	resourceType    string
	catalog         *PHICatalog
	placeholderJSON []byte
	strict          bool
	path            []string
	segmentIdx      *pathIndex
	catalogIdx      *pathIndex
	redactionSet    map[string]struct{}
	patchPaths      [][]string
}

// scrubJSONResource redacts PHI in one pass over JSON bytes using jsonparser.
func scrubJSONResource(
	resourceType string,
	data []byte,
	catalog *PHICatalog,
	segmentIdx *pathIndex,
	catalogIdx *pathIndex,
	placeholder string,
	strict bool,
) ([]byte, []string, error) {
	if len(data) == 0 {
		return data, nil, nil
	}
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	placeholderJSON, err := json.Marshal(placeholder)
	if err != nil {
		return nil, nil, err
	}
	s := &jsonScrubber{
		resourceType:    resourceType,
		catalog:         catalog,
		placeholderJSON: placeholderJSON,
		strict:          strict,
		segmentIdx:      segmentIdx,
		catalogIdx:      catalogIdx,
		redactionSet:    make(map[string]struct{}),
	}
	if err := s.walkValue(data, jsonparser.Object); err != nil {
		return nil, nil, err
	}
	if len(s.patchPaths) == 0 {
		return data, nil, nil
	}
	sort.Slice(s.patchPaths, func(i, j int) bool {
		return len(s.patchPaths[i]) > len(s.patchPaths[j])
	})
	out := data
	for _, path := range s.patchPaths {
		out, err = jsonparser.Set(out, s.placeholderJSON, jsonParserPath(path)...)
		if err != nil {
			return nil, nil, fmt.Errorf("json scrub set %v: %w", path, err)
		}
	}
	return out, s.redactionList(), nil
}

// jsonParserPath converts logical dot segments (with numeric indexes) to
// jsonparser path tokens (array indexes use bracket notation).
func jsonParserPath(segments []string) []string {
	if len(segments) == 0 {
		return nil
	}
	out := make([]string, len(segments))
	for i, seg := range segments {
		if isPathIndexSegment(seg) {
			out[i] = "[" + seg + "]"
		} else {
			out[i] = seg
		}
	}
	return out
}

func (s *jsonScrubber) redactionList() []string {
	if len(s.redactionSet) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.redactionSet))
	for k := range s.redactionSet {
		out = append(out, k)
	}
	return out
}

func (s *jsonScrubber) recordRedaction(fullPath string) {
	s.redactionSet[fmt.Sprintf("%s.%s", s.resourceType, fullPath)] = struct{}{}
}

func (s *jsonScrubber) scheduleRedaction(path []string, fullPath string) {
	s.patchPaths = append(s.patchPaths, append([]string(nil), path...))
	s.recordRedaction(fullPath)
}

func (s *jsonScrubber) pathIndicesMatch(norm string) bool {
	if s.segmentIdx != nil && s.segmentIdx.Match(norm) {
		return true
	}
	return s.catalogIdx != nil && s.catalogIdx.Match(norm)
}

func (s *jsonScrubber) walkValue(data []byte, dataType jsonparser.ValueType) error {
	switch dataType {
	case jsonparser.Object:
		return s.walkObject(data)
	case jsonparser.Array:
		return s.walkArray(data)
	default:
		return nil
	}
}

func (s *jsonScrubber) walkObject(data []byte) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
		keyStr := string(key)
		path := appendPath(s.path, keyStr)
		fullPath := joinPathSegments(path)
		norm := normalizePathIndexes(fullPath)
		if s.pathIndicesMatch(norm) {
			s.scheduleRedaction(path, fullPath)
			return nil
		}
		if isJSONScalar(dataType) {
			if s.catalog.passiveSensitiveKey(keyStr, norm) || (s.strict && s.shouldRedactStrictLeaf(keyStr, norm)) {
				s.scheduleRedaction(path, fullPath)
			}
			return nil
		}
		s.path = path
		err := s.walkValue(value, dataType)
		s.path = s.path[:len(s.path)-1]
		return err
	})
}

func (s *jsonScrubber) walkArray(data []byte) error {
	i := 0
	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, _ int, _ error) {
		idxStr := fmt.Sprintf("%d", i)
		i++
		path := appendPath(s.path, idxStr)
		fullPath := joinPathSegments(path)
		norm := normalizePathIndexes(fullPath)
		if dataType == jsonparser.Object {
			s.path = path
			_ = s.walkObject(value)
			s.path = s.path[:len(s.path)-1]
			return
		}
		if dataType == jsonparser.Array {
			s.path = path
			_ = s.walkArray(value)
			s.path = s.path[:len(s.path)-1]
			return
		}
		if s.strict && !s.catalog.pathAllowedInStrictMode(norm, idxStr) {
			s.scheduleRedaction(path, fullPath)
		}
	})
	return err
}

func isJSONScalar(dataType jsonparser.ValueType) bool {
	switch dataType {
	case jsonparser.String, jsonparser.Number, jsonparser.Boolean, jsonparser.Null:
		return true
	default:
		return false
	}
}

func (s *jsonScrubber) shouldRedactStrictLeaf(key, fullPath string) bool {
	if s.catalog.pathAllowedInStrictMode(fullPath, key) {
		return false
	}
	if s.catalog.alwaysAllowedKey(key) {
		return false
	}
	return true
}

func appendPath(prefix []string, segment string) []string {
	return append(append([]string(nil), prefix...), segment)
}

func joinPathSegments(parts []string) string {
	var out []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, ".")
}

func strictRedactionFromJSON(data []byte, catalog *PHICatalog) bool {
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	found := false
	_, _ = jsonparser.ArrayEach(data, func(elem []byte, _ jsonparser.ValueType, _ int, _ error) {
		if found {
			return
		}
		system, err := jsonparser.GetString(elem, "system")
		if err != nil {
			return
		}
		code, err := jsonparser.GetString(elem, "code")
		if err != nil || code == "" {
			return
		}
		if catalog.strictSecurityMatch(system, code) {
			found = true
		}
	}, "meta", "security")
	return found
}

func metaProfileURLsFromJSON(data []byte) []string {
	profileJSON, dataType, _, err := jsonparser.Get(data, "meta", "profile")
	if err != nil || len(profileJSON) == 0 {
		return nil
	}
	switch dataType {
	case jsonparser.Array:
		var out []string
		_, _ = jsonparser.ArrayEach(profileJSON, func(item []byte, itemType jsonparser.ValueType, _ int, _ error) {
			if itemType != jsonparser.String {
				return
			}
			s, err := jsonparser.ParseString(item)
			if err != nil || s == "" {
				return
			}
			out = append(out, s)
		})
		return out
	case jsonparser.String:
		s, err := jsonparser.ParseString(profileJSON)
		if err != nil || s == "" {
			return nil
		}
		return []string{s}
	default:
		return nil
	}
}

func resourceTypeFromJSON(data []byte, fallback string) string {
	rt, err := jsonparser.GetString(data, "resourceType")
	if err != nil || rt == "" {
		return fallback
	}
	return rt
}

func resourceMapForProfileIndex(data []byte) map[string]any {
	urls := metaProfileURLsFromJSON(data)
	if len(urls) == 0 {
		return nil
	}
	profiles := make([]any, len(urls))
	for i, u := range urls {
		profiles[i] = u
	}
	return map[string]any{
		"meta": map[string]any{
			"profile": profiles,
		},
	}
}

func mergeJSONIntoMap(dst map[string]any, data []byte) error {
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range decoded {
		dst[k] = v
	}
	return nil
}
