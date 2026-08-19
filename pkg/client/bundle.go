package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// BundleBuilder assembles transaction or batch request bundles.
type BundleBuilder struct {
	bundleType string
	entries    []bundleEntry
}

type bundleEntry struct {
	method   string
	url      string
	resource *types.ResourceEnvelope
	ifMatch  string
	fullURL  string
}

// NewTransactionBundleBuilder returns a builder for transaction bundles.
func NewTransactionBundleBuilder() *BundleBuilder {
	return &BundleBuilder{bundleType: "transaction"}
}

// NewBatchBundleBuilder returns a builder for batch bundles.
func NewBatchBundleBuilder() *BundleBuilder {
	return &BundleBuilder{bundleType: "batch"}
}

// CreateEntry adds a POST create entry.
func (b *BundleBuilder) CreateEntry(resource *types.ResourceEnvelope) *BundleBuilder {
	if b == nil || resource == nil {
		return b
	}
	b.entries = append(b.entries, bundleEntry{
		method:   "POST",
		url:      resource.ResourceType,
		resource: resource,
		fullURL:  resource.ResourceType + "/" + resource.ID,
	})
	return b
}

// ReadEntry adds a GET read entry.
func (b *BundleBuilder) ReadEntry(resourceType, id string) *BundleBuilder {
	if b == nil {
		return b
	}
	b.entries = append(b.entries, bundleEntry{
		method:  "GET",
		url:     resourceType + "/" + id,
		fullURL: resourceType + "/" + id,
	})
	return b
}

// UpdateEntry adds a PUT update entry.
func (b *BundleBuilder) UpdateEntry(resource *types.ResourceEnvelope) *BundleBuilder {
	if b == nil || resource == nil {
		return b
	}
	b.entries = append(b.entries, bundleEntry{
		method:   "PUT",
		url:      resource.ResourceType + "/" + resource.ID,
		resource: resource,
		fullURL:  resource.ResourceType + "/" + resource.ID,
	})
	return b
}

// DeleteEntry adds a DELETE entry.
func (b *BundleBuilder) DeleteEntry(resourceType, id string) *BundleBuilder {
	if b == nil {
		return b
	}
	b.entries = append(b.entries, bundleEntry{
		method:  "DELETE",
		url:     resourceType + "/" + id,
		fullURL: resourceType + "/" + id,
	})
	return b
}

// IfMatch sets If-Match on the last entry (for conditional updates).
func (b *BundleBuilder) IfMatch(versionID string) *BundleBuilder {
	if b == nil || len(b.entries) == 0 || versionID == "" {
		return b
	}
	b.entries[len(b.entries)-1].ifMatch = versionID
	return b
}

// Build returns a Bundle ResourceEnvelope ready for submission.
func (b *BundleBuilder) Build(codec types.ResourceCodec) (*types.ResourceEnvelope, error) {
	if b == nil {
		return nil, fmt.Errorf("bundle builder is nil")
	}
	if codec == nil {
		codec = types.NewJSONCodec()
	}
	entries := make([]map[string]interface{}, 0, len(b.entries))
	for _, e := range b.entries {
		entry := map[string]interface{}{
			"request": map[string]interface{}{
				"method": e.method,
				"url":    e.url,
			},
		}
		if e.fullURL != "" {
			entry["fullUrl"] = e.fullURL
		}
		if e.resource != nil {
			var resObj interface{}
			if err := json.Unmarshal(e.resource.JSON, &resObj); err != nil {
				return nil, fmt.Errorf("unmarshal resource: %w", err)
			}
			entry["resource"] = resObj
		}
		if e.ifMatch != "" {
			req := entry["request"].(map[string]interface{})
			req["ifMatch"] = "W/\"" + e.ifMatch + "\""
		}
		entries = append(entries, entry)
	}
	obj := map[string]interface{}{
		"resourceType": "Bundle",
		"type":         b.bundleType,
		"entry":        entries,
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return codec.ParseJSON("Bundle", data)
}

// Submit builds and submits the bundle as a transaction.
func (b *BundleBuilder) Submit(ctx context.Context, c *Client) (*types.ResourceEnvelope, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	bundle, err := b.Build(c.codec)
	if err != nil {
		return nil, err
	}
	if b.bundleType == "batch" {
		return c.Batch(ctx, bundle)
	}
	return c.Transaction(ctx, bundle)
}
