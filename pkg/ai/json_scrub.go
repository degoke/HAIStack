package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/buger/jsonparser"
)

// jsonScrubResolve supplies path indices and strict mode for one resource JSON
// object (root, contained item, or Bundle entry.resource).
type jsonScrubResolve func(resourceType string, resourceJSON []byte) (segmentIdx, catalogIdx *pathIndex, strict bool, err error)

type jsonScrubber struct {
	catalog         *PHICatalog
	placeholderJSON []byte
	resolve         jsonScrubResolve
	resourceType    string
	segmentIdx      *pathIndex
	catalogIdx      *pathIndex
	strict          bool
	docPath         []string
	resPath         []string
	contextStack    []jsonScrubContext
	redactionSet    map[string]struct{}
	patchKeys       map[string]struct{}
	patchPaths      [][]string
	walkErr         error
}

type jsonScrubContext struct {
	resourceType string
	segmentIdx   *pathIndex
	catalogIdx   *pathIndex
	strict       bool
	resPath      []string
}

// scrubJSONDocument redacts PHI in one jsonparser walk, including nested
// contained resources and Bundle entry.resource subtrees.
func scrubJSONDocument(
	data []byte,
	catalog *PHICatalog,
	placeholder string,
	fallbackResourceType string,
	resolve jsonScrubResolve,
) ([]byte, []string, error) {
	if len(data) == 0 {
		return data, nil, nil
	}
	if catalog == nil {
		catalog = DefaultPHICatalog()
	}
	if resolve == nil {
		resolve = catalogOnlyJSONResolve(catalog, fallbackResourceType)
	}
	placeholderJSON, err := json.Marshal(placeholder)
	if err != nil {
		return nil, nil, err
	}
	s := &jsonScrubber{
		catalog:         catalog,
		placeholderJSON: placeholderJSON,
		resolve:         resolve,
		redactionSet:    make(map[string]struct{}),
		patchKeys:       make(map[string]struct{}),
	}
	rt := resourceTypeFromJSON(data, fallbackResourceType)
	seg, cat, strict, err := resolve(rt, data)
	if err != nil {
		return nil, nil, err
	}
	s.resourceType = rt
	s.segmentIdx = seg
	s.catalogIdx = cat
	s.strict = strict
	if err := s.walkObject(data); err != nil {
		return nil, nil, err
	}
	if len(s.patchPaths) == 0 {
		return data, nil, nil
	}
	out, err := applyJSONPatches(data, s.patchPaths, placeholderJSON)
	if err != nil {
		return nil, nil, err
	}
	return out, s.redactionList(), nil
}

func catalogOnlyJSONResolve(catalog *PHICatalog, fallbackType string) jsonScrubResolve {
	return func(resourceType string, resourceJSON []byte) (*pathIndex, *pathIndex, bool, error) {
		if resourceType == "" {
			resourceType = resourceTypeFromJSON(resourceJSON, fallbackType)
		}
		idx := catalogPathIndex(catalog, resourceType)
		strict := strictRedactionFromJSON(resourceJSON, catalog)
		return idx, idx, strict, nil
	}
}

func applyJSONPatches(data []byte, paths [][]string, replacement []byte) ([]byte, error) {
	type span struct {
		start int
		end   int
	}
	seen := make(map[string]struct{}, len(paths))
	var spans []span
	for _, path := range paths {
		key := pathKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		start, end, err := patchByteSpan(data, path)
		if err != nil {
			return nil, fmt.Errorf("json scrub locate %v: %w", path, err)
		}
		spans = append(spans, span{start: start, end: end})
	}
	sort.Slice(spans, func(i, j int) bool {
		return spans[i].start > spans[j].start
	})
	out := data
	for _, sp := range spans {
		out = spliceJSONBytes(out, sp.start, sp.end, replacement)
	}
	return out, nil
}

func patchByteSpan(data []byte, path []string) (start, end int, err error) {
	value, dataType, offset, err := jsonparser.Get(data, jsonParserPath(path)...)
	if err != nil {
		return 0, 0, err
	}
	spanLen := len(value)
	if dataType == jsonparser.String {
		quoted, err := json.Marshal(string(value))
		if err != nil {
			return 0, 0, err
		}
		spanLen = len(quoted)
	}
	if offset < spanLen {
		return 0, 0, fmt.Errorf("invalid json patch offset for %v", path)
	}
	start = offset - spanLen
	return start, offset, nil
}

func spliceJSONBytes(data []byte, start, end int, insert []byte) []byte {
	if start < 0 || end < start || end > len(data) {
		return data
	}
	out := make([]byte, 0, len(data)-(end-start)+len(insert))
	out = append(out, data[:start]...)
	out = append(out, insert...)
	out = append(out, data[end:]...)
	return out
}

func pathKey(path []string) string {
	return strings.Join(path, "\x00")
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
	sort.Strings(out)
	return out
}

func (s *jsonScrubber) scheduleRedaction(path []string, fullPath string) {
	key := pathKey(path)
	if _, ok := s.patchKeys[key]; ok {
		return
	}
	s.patchKeys[key] = struct{}{}
	s.patchPaths = append(s.patchPaths, append([]string(nil), path...))
	s.redactionSet[fmt.Sprintf("%s.%s", s.resourceType, fullPath)] = struct{}{}
}

func (s *jsonScrubber) pathIndicesMatch(norm string) bool {
	if s.segmentIdx != nil && s.segmentIdx.Match(norm) {
		return true
	}
	return s.catalogIdx != nil && s.catalogIdx.Match(norm)
}

func (s *jsonScrubber) pushContext() {
	s.contextStack = append(s.contextStack, jsonScrubContext{
		resourceType: s.resourceType,
		segmentIdx:   s.segmentIdx,
		catalogIdx:   s.catalogIdx,
		strict:       s.strict,
		resPath:      append([]string(nil), s.resPath...),
	})
}

func (s *jsonScrubber) popContext() {
	if len(s.contextStack) == 0 {
		return
	}
	top := s.contextStack[len(s.contextStack)-1]
	s.contextStack = s.contextStack[:len(s.contextStack)-1]
	s.resourceType = top.resourceType
	s.segmentIdx = top.segmentIdx
	s.catalogIdx = top.catalogIdx
	s.strict = top.strict
	s.resPath = top.resPath
}

func (s *jsonScrubber) withResourceContext(resourceJSON []byte, fallback string, fn func() error) error {
	rt := resourceTypeFromJSON(resourceJSON, fallback)
	seg, cat, strict, err := s.resolve(rt, resourceJSON)
	if err != nil {
		return err
	}
	s.pushContext()
	s.resourceType = rt
	s.segmentIdx = seg
	s.catalogIdx = cat
	s.strict = strict
	s.resPath = nil
	err = fn()
	s.popContext()
	return err
}

func (s *jsonScrubber) walkObject(data []byte) error {
	return jsonparser.ObjectEach(data, func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
		if s.walkErr != nil {
			return nil
		}
		keyStr := string(key)
		docPath := appendPath(s.docPath, keyStr)
		resPath := appendPath(s.resPath, keyStr)
		fullPath := joinPathSegments(resPath)
		norm := normalizePathIndexes(fullPath)
		if s.pathIndicesMatch(norm) {
			s.scheduleRedaction(docPath, fullPath)
			return nil
		}
		if keyStr == "contained" && dataType == jsonparser.Array {
			s.docPath = docPath
			s.resPath = resPath
			if err := s.walkNestedResourceArray(value); err != nil {
				return err
			}
			s.docPath = s.docPath[:len(s.docPath)-1]
			s.resPath = s.resPath[:len(s.resPath)-1]
			return nil
		}
		if keyStr == "resource" && dataType == jsonparser.Object && pathIsBundleEntryResource(s.docPath) {
			s.docPath = docPath
			err := s.withResourceContext(value, "", func() error {
				return s.walkObject(value)
			})
			s.docPath = s.docPath[:len(s.docPath)-1]
			return err
		}
		if isJSONScalar(dataType) {
			if s.catalog.passiveSensitiveKey(keyStr, norm) || (s.strict && s.shouldRedactStrictLeaf(keyStr, norm)) {
				s.scheduleRedaction(docPath, fullPath)
			}
			return nil
		}
		s.docPath = docPath
		s.resPath = resPath
		err := s.walkValue(value, dataType)
		s.docPath = s.docPath[:len(s.docPath)-1]
		s.resPath = s.resPath[:len(s.resPath)-1]
		return err
	})
}

func (s *jsonScrubber) walkNestedResourceArray(data []byte) error {
	i := 0
	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, _ int, _ error) {
		if s.walkErr != nil || dataType != jsonparser.Object {
			return
		}
		idxStr := fmt.Sprintf("%d", i)
		i++
		childDoc := appendPath(s.docPath, idxStr)
		childRes := appendPath(s.resPath, idxStr)
		s.docPath = childDoc
		s.resPath = childRes
		if err := s.withResourceContext(value, "", func() error {
			return s.walkObject(value)
		}); err != nil {
			s.walkErr = err
		}
		s.docPath = s.docPath[:len(s.docPath)-1]
		s.resPath = s.resPath[:len(s.resPath)-1]
	})
	if s.walkErr != nil {
		return s.walkErr
	}
	return err
}

func pathIsBundleEntryResource(parentPath []string) bool {
	if len(parentPath) < 2 {
		return false
	}
	if parentPath[len(parentPath)-2] != "entry" {
		return false
	}
	return isPathIndexSegment(parentPath[len(parentPath)-1])
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

func (s *jsonScrubber) walkArray(data []byte) error {
	i := 0
	_, err := jsonparser.ArrayEach(data, func(value []byte, dataType jsonparser.ValueType, _ int, _ error) {
		if s.walkErr != nil {
			return
		}
		idxStr := fmt.Sprintf("%d", i)
		i++
		docPath := appendPath(s.docPath, idxStr)
		resPath := appendPath(s.resPath, idxStr)
		fullPath := joinPathSegments(resPath)
		norm := normalizePathIndexes(fullPath)
		if dataType == jsonparser.Object {
			s.docPath = docPath
			s.resPath = resPath
			if err := s.walkObject(value); err != nil {
				s.walkErr = err
			}
			s.docPath = s.docPath[:len(s.docPath)-1]
			s.resPath = s.resPath[:len(s.resPath)-1]
			return
		}
		if dataType == jsonparser.Array {
			s.docPath = docPath
			s.resPath = resPath
			if err := s.walkArray(value); err != nil {
				s.walkErr = err
			}
			s.docPath = s.docPath[:len(s.docPath)-1]
			s.resPath = s.resPath[:len(s.resPath)-1]
			return
		}
		if s.strict && !s.catalog.pathAllowedInStrictMode(norm, idxStr) {
			s.scheduleRedaction(docPath, fullPath)
		}
	})
	if s.walkErr != nil {
		return s.walkErr
	}
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
