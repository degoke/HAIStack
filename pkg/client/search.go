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

// SearchIterator walks resources across search pages while retaining the
// underlying page metadata for callers that need it.
type SearchIterator struct {
	client       *Client
	ctx          context.Context
	resourceType string
	params       map[string]string
	page         *SearchResult
	index        int
	current      *types.ResourceEnvelope
	err          error
	started      bool
	seenPages    map[string]struct{}
}

// IterateSearch returns a resource iterator for a type-level search.
func (c *Client) IterateSearch(ctx context.Context, resourceType string, params map[string]string) *SearchIterator {
	return &SearchIterator{client: c, ctx: ctx, resourceType: resourceType, params: params}
}

// Next advances the iterator to the next resource.
func (it *SearchIterator) Next() bool {
	if it == nil || it.err != nil {
		return false
	}
	if it.client == nil {
		it.err = fmt.Errorf("search iterator client is nil")
		return false
	}
	it.current = nil
	for {
		if !it.started {
			it.started = true
			it.page, it.err = it.client.Search(it.ctx, it.resourceType, it.params)
		}
		if it.err != nil {
			return false
		}
		if it.page != nil && it.index < len(it.page.Entries) {
			it.current = it.page.Entries[it.index]
			it.index++
			return it.current != nil
		}
		if it.page != nil && it.page.HasNext() {
			resolved, err := resolveSameOriginURL(it.client.baseURL, it.page.NextURL)
			if err != nil {
				it.err = fmt.Errorf("search page URL: %w", err)
				return false
			}
			if it.seenPages == nil {
				it.seenPages = make(map[string]struct{})
			}
			if _, exists := it.seenPages[resolved]; exists {
				it.err = fmt.Errorf("search pagination cycle at %q", resolved)
				return false
			}
			it.seenPages[resolved] = struct{}{}
			it.page, it.err = it.client.SearchPage(it.ctx, it.resourceType, resolved)
			it.index = 0
		} else {
			return false
		}
	}
}

// Resource returns the resource selected by the most recent successful Next.
func (it *SearchIterator) Resource() *types.ResourceEnvelope {
	if it == nil {
		return nil
	}
	return it.current
}

// Err returns the terminal iterator error, if any.
func (it *SearchIterator) Err() error {
	if it == nil {
		return fmt.Errorf("search iterator is nil")
	}
	return it.err
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

// SearchPost performs a POST-based search using an URL-encoded parameter body.
func (c *Client) SearchPost(ctx context.Context, resourceType string, params map[string]string) (*SearchResult, error) {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	u := c.fhirURL(resourceType, "_search")
	raw, err := c.do(ctx, requestOptions{
		method:      "POST",
		url:         u,
		body:        []byte(values.Encode()),
		contentType: "application/x-www-form-urlencoded",
	})
	if err != nil {
		return nil, err
	}
	return parseSearchBundle(c.codec, resourceType, raw.Body)
}

// SearchPage fetches a search result from an absolute or relative next URL.
func (c *Client) SearchPage(ctx context.Context, resourceType, pageURL string) (*SearchResult, error) {
	resolved, err := resolveSameOriginURL(c.baseURL, pageURL)
	if err != nil {
		return nil, fmt.Errorf("search page URL: %w", err)
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
	seenPages := make(map[string]struct{})
	for page.HasNext() {
		resolved, resolveErr := resolveSameOriginURL(c.baseURL, page.NextURL)
		if resolveErr != nil {
			return nil, fmt.Errorf("search page URL: %w", resolveErr)
		}
		if _, exists := seenPages[resolved]; exists {
			return nil, fmt.Errorf("search pagination cycle at %q", resolved)
		}
		seenPages[resolved] = struct{}{}
		page, err = c.SearchPage(ctx, resourceType, resolved)
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
	seenPages := make(map[string]struct{})
	for page.HasNext() {
		resolved, resolveErr := resolveSameOriginURL(b.client.baseURL, page.NextURL)
		if resolveErr != nil {
			return nil, fmt.Errorf("search page URL: %w", resolveErr)
		}
		if _, exists := seenPages[resolved]; exists {
			return nil, fmt.Errorf("search pagination cycle at %q", resolved)
		}
		seenPages[resolved] = struct{}{}
		page, err = b.client.SearchPage(ctx, b.resourceType, resolved)
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
