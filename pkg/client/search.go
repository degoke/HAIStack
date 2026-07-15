package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// SearchResult is a page-aware search result with bundle metadata.
type SearchResult struct {
	ResourceType string
	Entries      []*types.ResourceEnvelope
	Total        *int
	NextURL      string
	SelfURL      string
	RawBundle    []byte
}

// HasNext reports whether another page is available.
func (r *SearchResult) HasNext() bool {
	return r != nil && r.NextURL != ""
}

// Search performs a type-level search with query parameters.
func (c *Client) Search(ctx context.Context, resourceType string, params map[string]string) (*SearchResult, error) {
	u := c.fhirURL(resourceType)
	if len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		u += "?" + values.Encode()
	}
	return c.searchURL(ctx, resourceType, u)
}

// SearchPage fetches a search result from an absolute or relative next URL.
func (c *Client) SearchPage(ctx context.Context, resourceType, pageURL string) (*SearchResult, error) {
	resolved := pageURL
	if !strings.HasPrefix(pageURL, "http://") && !strings.HasPrefix(pageURL, "https://") {
		if strings.HasPrefix(pageURL, "/") {
			resolved = c.baseURL + pageURL
		} else {
			resolved = c.baseURL + "/" + strings.TrimLeft(pageURL, "/")
		}
	}
	return c.searchURL(ctx, resourceType, resolved)
}

// SearchAll auto-paginates through all search results.
func (c *Client) SearchAll(ctx context.Context, resourceType string, params map[string]string) ([]*types.ResourceEnvelope, error) {
	page, err := c.Search(ctx, resourceType, params)
	if err != nil {
		return nil, err
	}
	all := append([]*types.ResourceEnvelope(nil), page.Entries...)
	for page.HasNext() {
		page, err = c.SearchPage(ctx, resourceType, page.NextURL)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Entries...)
	}
	return all, nil
}

// SearchBuilder returns a fluent search query builder for a resource type.
func (c *Client) SearchBuilder(resourceType string) *SearchBuilder {
	return &SearchBuilder{
		client:       c,
		resourceType: resourceType,
		values:       make(url.Values),
	}
}

// SearchBuilder composes FHIR search query parameters fluently.
type SearchBuilder struct {
	client       *Client
	resourceType string
	values       url.Values
}

// Param sets a single-valued search parameter.
func (b *SearchBuilder) Param(name, value string) *SearchBuilder {
	if b != nil {
		b.values.Set(name, value)
	}
	return b
}

// AddParam appends a repeated search parameter value.
func (b *SearchBuilder) AddParam(name, value string) *SearchBuilder {
	if b != nil {
		b.values.Add(name, value)
	}
	return b
}

// OrParam sets a comma-separated OR value for a parameter.
func (b *SearchBuilder) OrParam(name string, values ...string) *SearchBuilder {
	if b != nil && len(values) > 0 {
		b.values.Set(name, strings.Join(values, ","))
	}
	return b
}

// Count sets _count.
func (b *SearchBuilder) Count(n int) *SearchBuilder {
	if b != nil {
		b.values.Set("_count", strconv.Itoa(n))
	}
	return b
}

// Sort sets _sort.
func (b *SearchBuilder) Sort(fields ...string) *SearchBuilder {
	if b != nil && len(fields) > 0 {
		b.values.Set("_sort", strings.Join(fields, ","))
	}
	return b
}

// Values returns the composed url.Values.
func (b *SearchBuilder) Values() url.Values {
	if b == nil {
		return nil
	}
	out := make(url.Values, len(b.values))
	for k, vs := range b.values {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

// Search executes the built query.
func (b *SearchBuilder) Search(ctx context.Context) (*SearchResult, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("search builder is nil")
	}
	u := b.client.fhirURL(b.resourceType) + "?" + b.values.Encode()
	return b.client.searchURL(ctx, b.resourceType, u)
}

// SearchAll auto-paginates the built query.
func (b *SearchBuilder) SearchAll(ctx context.Context) ([]*types.ResourceEnvelope, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("search builder is nil")
	}
	page, err := b.Search(ctx)
	if err != nil {
		return nil, err
	}
	all := append([]*types.ResourceEnvelope(nil), page.Entries...)
	for page.HasNext() {
		page, err = b.client.SearchPage(ctx, b.resourceType, page.NextURL)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Entries...)
	}
	return all, nil
}

func (c *Client) searchURL(ctx context.Context, resourceType, u string) (*SearchResult, error) {
	raw, err := c.do(ctx, requestOptions{
		method: "GET",
		url:    u,
	})
	if err != nil {
		return nil, err
	}
	return parseSearchBundle(c.codec, resourceType, raw.Body)
}

func parseSearchBundle(codec types.ResourceCodec, resourceType string, data []byte) (*SearchResult, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	rt, _ := obj["resourceType"].(string)
	if rt != "Bundle" {
		return nil, fmt.Errorf("expected Bundle, got %q", rt)
	}

	result := &SearchResult{
		ResourceType: resourceType,
		RawBundle:    append([]byte(nil), data...),
	}
	if totalVal, ok := obj["total"].(float64); ok {
		t := int(totalVal)
		result.Total = &t
	}
	if links, ok := obj["link"].([]interface{}); ok {
		for _, link := range links {
			lm, ok := link.(map[string]interface{})
			if !ok {
				continue
			}
			relation, _ := lm["relation"].(string)
			linkURL, _ := lm["url"].(string)
			switch relation {
			case "next":
				result.NextURL = linkURL
			case "self":
				result.SelfURL = linkURL
			}
		}
	}
	entries, ok := obj["entry"].([]interface{})
	if !ok {
		return result, nil
	}
	for _, entry := range entries {
		em, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		resObj, ok := em["resource"].(map[string]interface{})
		if !ok {
			continue
		}
		resBytes, err := json.Marshal(resObj)
		if err != nil {
			return nil, err
		}
		entryType, _ := resObj["resourceType"].(string)
		if entryType == "" {
			entryType = resourceType
		}
		env, err := codec.ParseJSON(entryType, resBytes)
		if err != nil {
			return nil, err
		}
		result.Entries = append(result.Entries, env)
	}
	return result, nil
}
