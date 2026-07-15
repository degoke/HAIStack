package client

import (
	"context"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// Create posts a new resource and returns the server-assigned envelope.
func (c *Client) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	body, err := c.codec.ToJSON(resource)
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, requestOptions{
		method: "POST",
		url:    c.fhirURL(resource.ResourceType),
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resource.ResourceType, raw.Body)
}

// Read fetches a resource by type and id.
func (c *Client) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	raw, err := c.do(ctx, requestOptions{
		method: "GET",
		url:    c.fhirURL(resourceType, id),
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resourceType, raw.Body)
}

// Update replaces a resource by type and id.
func (c *Client) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	body, err := c.codec.ToJSON(resource)
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, requestOptions{
		method: "PUT",
		url:    c.fhirURL(resource.ResourceType, resource.ID),
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resource.ResourceType, raw.Body)
}

// Delete removes a resource by type and id.
func (c *Client) Delete(ctx context.Context, resourceType, id string) error {
	_, err := c.do(ctx, requestOptions{
		method:      "DELETE",
		url:         c.fhirURL(resourceType, id),
		expectEmpty: true,
	})
	return err
}

// Transaction submits a transaction bundle and returns the response bundle envelope.
func (c *Client) Transaction(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle is nil")
	}
	body, err := c.codec.ToJSON(bundle)
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, requestOptions{
		method: "POST",
		url:    c.fhirURL(),
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON("Bundle", raw.Body)
}

// ResourceClient provides typed convenience methods for one resource type.
type ResourceClient struct {
	client       *Client
	resourceType string
}

// ResourceType returns the bound resource type.
func (r *ResourceClient) ResourceType() string {
	if r == nil {
		return ""
	}
	return r.resourceType
}

// Create creates a resource of the bound type.
func (r *ResourceClient) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	if resource != nil {
		resource.ResourceType = r.resourceType
	}
	return r.client.Create(ctx, resource)
}

// Read reads a resource of the bound type.
func (r *ResourceClient) Read(ctx context.Context, id string) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.Read(ctx, r.resourceType, id)
}

// Update updates a resource of the bound type.
func (r *ResourceClient) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	if resource != nil {
		resource.ResourceType = r.resourceType
	}
	return r.client.Update(ctx, resource)
}

// Delete deletes a resource of the bound type.
func (r *ResourceClient) Delete(ctx context.Context, id string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("resource client is nil")
	}
	return r.client.Delete(ctx, r.resourceType, id)
}

// Search searches resources of the bound type.
func (r *ResourceClient) Search(ctx context.Context, params map[string]string) (*SearchResult, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.Search(ctx, r.resourceType, params)
}

// SearchAll auto-paginates search results for the bound type.
func (r *ResourceClient) SearchAll(ctx context.Context, params map[string]string) ([]*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.SearchAll(ctx, r.resourceType, params)
}

// SearchBuilder returns a fluent search builder for the bound type.
func (r *ResourceClient) SearchBuilder() *SearchBuilder {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.SearchBuilder(r.resourceType)
}
