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
	Included     []IncludedEntry
	Total        *int
	Offset       int
	Count        int
	Summary      SummaryMode
	Elements     []string
	Links        map[string]string
}

// IncludedEntry is one included or revincluded resource.
type IncludedEntry struct {
	ResourceType string
	ID           string
	Resource     *types.ResourceEnvelope
	Mode         string
}

// BundleEntry is one searchset bundle entry.
type BundleEntry struct {
	FullURL  string
	Resource *types.ResourceEnvelope
	Mode     string
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
			Mode:     "match",
		})
	}
	for _, inc := range result.Included {
		mode := inc.Mode
		if mode == "" {
			mode = "include"
		}
		bundle.Entries = append(bundle.Entries, BundleEntry{
			FullURL:  fmt.Sprintf("%s/%s", inc.ResourceType, inc.ID),
			Resource: inc.Resource,
			Mode:     mode,
		})
	}
	return bundle
}

// BuildPagingLinks constructs self and next links preserving the original query parameters.
func BuildPagingLinks(baseURL string, params url.Values, offset, count, pageSize int, total *int) map[string]string {
	query := cloneValues(params)
	query.Set("_offset", fmt.Sprintf("%d", offset))
	query.Set("_count", fmt.Sprintf("%d", pageSize))
	links := map[string]string{
		"self": baseURL + "?" + query.Encode(),
	}
	hasNext := false
	if total != nil {
		hasNext = offset+count < *total
	} else if count == pageSize {
		hasNext = true
	}
	if hasNext {
		nextQuery := cloneValues(params)
		nextQuery.Set("_offset", fmt.Sprintf("%d", offset+count))
		nextQuery.Set("_count", fmt.Sprintf("%d", pageSize))
		links["next"] = baseURL + "?" + nextQuery.Encode()
	}
	return links
}

func cloneValues(in url.Values) url.Values {
	out := url.Values{}
	for k, vs := range in {
		for _, v := range vs {
			if k == "_offset" || k == "_count" {
				continue
			}
			out.Add(k, v)
		}
	}
	return out
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
