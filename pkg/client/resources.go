package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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

// UpdateIfMatch replaces a resource only when versionID still matches the
// server's current weak ETag.
func (c *Client) UpdateIfMatch(ctx context.Context, resource *types.ResourceEnvelope, versionID string) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	if strings.TrimSpace(versionID) == "" {
		return nil, fmt.Errorf("versionID is required")
	}
	body, err := c.codec.ToJSON(resource)
	if err != nil {
		return nil, err
	}
	raw, err := c.do(ctx, requestOptions{
		method: http.MethodPut,
		url:    c.fhirURL(resource.ResourceType, resource.ID),
		body:   body,
		headers: map[string]string{
			"If-Match": weakETag(versionID),
		},
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resource.ResourceType, raw.Body)
}

// CreateJSON creates a resource from raw FHIR JSON and returns raw response JSON.
func (c *Client) CreateJSON(ctx context.Context, resourceType string, body []byte) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	resource, err := c.codec.ParseJSON(resourceType, body)
	if err != nil {
		return nil, err
	}
	created, err := c.Create(ctx, resource)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), created.JSON...), nil
}

// ReadJSON reads a resource and returns raw response JSON.
func (c *Client) ReadJSON(ctx context.Context, resourceType, id string) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	resource, err := c.Read(ctx, resourceType, id)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), resource.JSON...), nil
}

// UpdateJSON replaces a resource from raw FHIR JSON and returns raw response JSON.
func (c *Client) UpdateJSON(ctx context.Context, resourceType, id string, body []byte) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	var object map[string]any
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	if object == nil {
		return nil, fmt.Errorf("resource JSON must be an object")
	}
	if bodyID, ok := object["id"].(string); ok && bodyID != "" && bodyID != id {
		return nil, fmt.Errorf("resource id %q does not match path id %q", bodyID, id)
	}
	object["id"] = id
	normalized, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	resource, err := c.codec.ParseJSON(resourceType, normalized)
	if err != nil {
		return nil, err
	}
	resource.ID = id
	updated, err := c.Update(ctx, resource)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), updated.JSON...), nil
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

// Patch applies an RFC 6902 JSON Patch to a resource.
func (c *Client) Patch(ctx context.Context, resourceType, id string, patchJSON []byte) (*types.ResourceEnvelope, error) {
	if len(patchJSON) == 0 {
		return nil, fmt.Errorf("patch is empty")
	}
	raw, err := c.do(ctx, requestOptions{
		method:      "PATCH",
		url:         c.fhirURL(resourceType, id),
		body:        patchJSON,
		contentType: "application/json-patch+json",
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resourceType, raw.Body)
}

// PatchIfMatch applies JSON Patch only when versionID still matches the
// server's current weak ETag.
func (c *Client) PatchIfMatch(ctx context.Context, resourceType, id string, patchJSON []byte, versionID string) (*types.ResourceEnvelope, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, fmt.Errorf("versionID is required")
	}
	if len(patchJSON) == 0 {
		return nil, fmt.Errorf("patch is empty")
	}
	raw, err := c.do(ctx, requestOptions{
		method:      http.MethodPatch,
		url:         c.fhirURL(resourceType, id),
		body:        patchJSON,
		contentType: "application/json-patch+json",
		headers:     map[string]string{"If-Match": weakETag(versionID)},
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resourceType, raw.Body)
}

// DeleteIfMatch deletes a resource only when versionID still matches the
// server's current weak ETag.
func (c *Client) DeleteIfMatch(ctx context.Context, resourceType, id, versionID string) error {
	if strings.TrimSpace(versionID) == "" {
		return fmt.Errorf("versionID is required")
	}
	_, err := c.do(ctx, requestOptions{
		method:      http.MethodDelete,
		url:         c.fhirURL(resourceType, id),
		headers:     map[string]string{"If-Match": weakETag(versionID)},
		expectEmpty: true,
	})
	return err
}

func weakETag(versionID string) string {
	versionID = strings.TrimSpace(versionID)
	if versionID == "*" {
		return versionID
	}
	if strings.HasPrefix(versionID, "W/\"") || strings.HasPrefix(versionID, "\"") {
		return versionID
	}
	return `W/"` + versionID + `"`
}

// Operation executes a generic server-defined FHIR operation. A nil body uses
// GET; a non-nil body uses POST. The operation must include its leading '$'.
func (c *Client) Operation(ctx context.Context, resourceType, id, operation string, params url.Values, body []byte) (*types.ResourceEnvelope, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	if len(operation) < 2 || operation[0] != '$' {
		return nil, fmt.Errorf("operation must be a valid $ operation")
	}
	for _, r := range operation[1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return nil, fmt.Errorf("operation must be a valid $ operation")
		}
	}
	parts := make([]string, 0, 3)
	if resourceType != "" {
		parts = append(parts, resourceType)
		if id != "" {
			parts = append(parts, id)
		}
	}
	parts = append(parts, operation)
	u := c.fhirURL(parts...)
	if encoded := params.Encode(); encoded != "" {
		u += "?" + encoded
	}
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}
	raw, err := c.do(ctx, requestOptions{
		method: method,
		url:    u,
		body:   body,
	})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON("", raw.Body)
}

// CreateConditional performs conditional create using the FHIR If-None-Exist header.
func (c *Client) CreateConditional(ctx context.Context, resource *types.ResourceEnvelope, criteria url.Values) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	body, err := c.codec.ToJSON(resource)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	if encoded := criteria.Encode(); encoded != "" {
		headers["If-None-Exist"] = encoded
	}
	raw, err := c.do(ctx, requestOptions{method: "POST", url: c.fhirURL(resource.ResourceType), body: body, headers: headers})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resource.ResourceType, raw.Body)
}

// UpdateConditional replaces the single resource selected by search criteria.
func (c *Client) UpdateConditional(ctx context.Context, resource *types.ResourceEnvelope, criteria url.Values) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is nil")
	}
	body, err := c.codec.ToJSON(resource)
	if err != nil {
		return nil, err
	}
	u := c.fhirURL(resource.ResourceType)
	if encoded := criteria.Encode(); encoded != "" {
		u += "?" + encoded
	}
	raw, err := c.do(ctx, requestOptions{method: "PUT", url: u, body: body})
	if err != nil {
		return nil, err
	}
	return c.codec.ParseJSON(resource.ResourceType, raw.Body)
}

// DeleteConditional deletes the single resource selected by search criteria.
func (c *Client) DeleteConditional(ctx context.Context, resourceType string, criteria url.Values) error {
	u := c.fhirURL(resourceType)
	if encoded := criteria.Encode(); encoded != "" {
		u += "?" + encoded
	}
	_, err := c.do(ctx, requestOptions{method: "DELETE", url: u, expectEmpty: true})
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

// Batch submits a batch bundle and returns its batch-response Bundle.
func (c *Client) Batch(ctx context.Context, bundle *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	return c.submitBundle(ctx, bundle, "batch")
}

func (c *Client) submitBundle(ctx context.Context, bundle *types.ResourceEnvelope, expectedType string) (*types.ResourceEnvelope, error) {
	if bundle == nil {
		return nil, fmt.Errorf("bundle is nil")
	}
	body, err := c.codec.ToJSON(bundle)
	if err != nil {
		return nil, err
	}
	var shape struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &shape); err != nil {
		return nil, err
	}
	if shape.Type != expectedType {
		return nil, fmt.Errorf("bundle type is %q, want %q", shape.Type, expectedType)
	}
	raw, err := c.do(ctx, requestOptions{method: "POST", url: c.fhirURL(), body: body})
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

// UpdateIfMatch replaces a bound resource when its version still matches.
func (r *ResourceClient) UpdateIfMatch(ctx context.Context, resource *types.ResourceEnvelope, versionID string) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	if resource != nil {
		resource.ResourceType = r.resourceType
	}
	return r.client.UpdateIfMatch(ctx, resource, versionID)
}

// CreateJSON creates a bound resource from raw FHIR JSON.
func (r *ResourceClient) CreateJSON(ctx context.Context, body []byte) ([]byte, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.CreateJSON(ctx, r.resourceType, body)
}

// ReadJSON reads a bound resource as raw FHIR JSON.
func (r *ResourceClient) ReadJSON(ctx context.Context, id string) ([]byte, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.ReadJSON(ctx, r.resourceType, id)
}

// UpdateJSON replaces a bound resource from raw FHIR JSON.
func (r *ResourceClient) UpdateJSON(ctx context.Context, id string, body []byte) ([]byte, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.UpdateJSON(ctx, r.resourceType, id, body)
}

// Delete deletes a resource of the bound type.
func (r *ResourceClient) Delete(ctx context.Context, id string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("resource client is nil")
	}
	return r.client.Delete(ctx, r.resourceType, id)
}

// Patch applies an RFC 6902 JSON Patch to the bound resource type.
func (r *ResourceClient) Patch(ctx context.Context, id string, patchJSON []byte) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.Patch(ctx, r.resourceType, id, patchJSON)
}

// PatchIfMatch applies JSON Patch to a bound resource when its version still matches.
func (r *ResourceClient) PatchIfMatch(ctx context.Context, id string, patchJSON []byte, versionID string) (*types.ResourceEnvelope, error) {
	if r == nil || r.client == nil {
		return nil, fmt.Errorf("resource client is nil")
	}
	return r.client.PatchIfMatch(ctx, r.resourceType, id, patchJSON, versionID)
}

// DeleteIfMatch deletes a bound resource when its version still matches.
func (r *ResourceClient) DeleteIfMatch(ctx context.Context, id, versionID string) error {
	if r == nil || r.client == nil {
		return fmt.Errorf("resource client is nil")
	}
	return r.client.DeleteIfMatch(ctx, r.resourceType, id, versionID)
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
