package search

import (
	"fmt"
	"net/url"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Result holds search matches and paging metadata for bundle assembly.
type Result struct {
	ResourceType string
	Resources    []*types.ResourceEnvelope
	Total        *int
	Offset       int
	Count        int
	Links        map[string]string
}

// BundleEntry is one searchset bundle entry.
type BundleEntry struct {
	FullURL  string
	Resource *types.ResourceEnvelope
}

// SearchBundle is a bundle-ready searchset payload.
type SearchBundle struct {
	ResourceType string
	Total        *int
	Offset       int
	Count        int
	Links        map[string]string
	Entries      []BundleEntry
}

// AssembleBundle converts a search Result into bundle-ready output.
func AssembleBundle(result *Result) *SearchBundle {
	if result == nil {
		return &SearchBundle{}
	}
	bundle := &SearchBundle{
		ResourceType: result.ResourceType,
		Total:        result.Total,
		Offset:       result.Offset,
		Count:        len(result.Resources),
		Links:        cloneLinks(result.Links),
	}
	for _, res := range result.Resources {
		bundle.Entries = append(bundle.Entries, BundleEntry{
			FullURL:  fmt.Sprintf("%s/%s", res.ResourceType, res.ID),
			Resource: res,
		})
	}
	return bundle
}

// BuildPagingLinks constructs self and next links for offset-based paging.
// total is the number of matches before pagination; when nil, total behavior is partial/unknown.
func BuildPagingLinks(baseURL string, offset, count, pageSize int, total *int) map[string]string {
	links := map[string]string{
		"self": fmt.Sprintf("%s?_offset=%d&_count=%d", baseURL, offset, pageSize),
	}
	hasNext := false
	if total != nil {
		hasNext = offset+count < *total
	} else if count == pageSize {
		hasNext = true
	}
	if hasNext {
		links["next"] = fmt.Sprintf("%s?_offset=%d&_count=%d", baseURL, offset+count, pageSize)
	}
	return links
}

func cloneLinks(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// ParseQueryValues is a helper for map-based query params in tests and callers without url.Values.
func ParseQueryValues(resourceType string, params map[string][]string) (*Query, error) {
	values := url.Values{}
	for k, v := range params {
		for _, item := range v {
			values.Add(k, item)
		}
	}
	return ParseQuery(resourceType, values)
}
