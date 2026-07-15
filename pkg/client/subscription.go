package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// SubscriptionClient provides standard FHIR Subscription REST helpers.
type SubscriptionClient struct {
	client       *Client
	resourceType string
}

// Create creates a Subscription resource.
func (s *SubscriptionClient) Create(ctx context.Context, subscription *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	if subscription != nil {
		subscription.ResourceType = s.resourceType
	}
	return s.client.Create(ctx, subscription)
}

// Read reads a Subscription by id.
func (s *SubscriptionClient) Read(ctx context.Context, id string) (*types.ResourceEnvelope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	return s.client.Read(ctx, s.resourceType, id)
}

// Update updates a Subscription resource.
func (s *SubscriptionClient) Update(ctx context.Context, subscription *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	if subscription != nil {
		subscription.ResourceType = s.resourceType
	}
	return s.client.Update(ctx, subscription)
}

// Delete deletes a Subscription by id.
func (s *SubscriptionClient) Delete(ctx context.Context, id string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("subscription client is nil")
	}
	return s.client.Delete(ctx, s.resourceType, id)
}

// Search searches Subscription resources.
func (s *SubscriptionClient) Search(ctx context.Context, params map[string]string) (*SearchResult, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	return s.client.Search(ctx, s.resourceType, params)
}

// SearchAll auto-paginates Subscription search results.
func (s *SubscriptionClient) SearchAll(ctx context.Context, params map[string]string) ([]*types.ResourceEnvelope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	return s.client.SearchAll(ctx, s.resourceType, params)
}

// PollStatus polls a Subscription status endpoint when the server exposes one.
// statusURL may be absolute or relative to the client base URL.
func (s *SubscriptionClient) PollStatus(ctx context.Context, statusURL string) (*types.ResourceEnvelope, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("subscription client is nil")
	}
	raw, err := s.client.do(ctx, requestOptions{
		method: "GET",
		url:    resolveURL(s.client.baseURL, statusURL),
	})
	if err != nil {
		return nil, err
	}
	return s.client.codec.ParseJSON("SubscriptionStatus", raw.Body)
}

func resolveURL(base, u string) string {
	if u == "" {
		return u
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	if strings.HasPrefix(u, "/") {
		return base + u
	}
	return base + "/" + u
}
