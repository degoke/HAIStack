package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// checkReferentialIntegrity requires local typed relative references to exist
// before persist. Contained fragments, absolute URLs, URNs, untyped ids, and
// self-references are skipped. Resources written earlier in the same write
// session (including transaction-bundle entries) satisfy Exists.
func (s *ResourceService) checkReferentialIntegrity(ctx context.Context, session store.WriteSession, envelope *types.ResourceEnvelope) error {
	if envelope == nil {
		return nil
	}
	refs, err := types.GetReferences(envelope.JSON)
	if err != nil {
		return invalidErr("parse resource references", err)
	}
	resources := session.ResourceStore()
	for _, ref := range refs {
		targetType, targetID, ok := localTypedReference(ref, envelope)
		if !ok {
			continue
		}
		exists, err := resources.Exists(ctx, targetType, targetID)
		if err != nil {
			return exceptionErr("check referenced resource", err)
		}
		if !exists {
			return invalidErr(fmt.Sprintf("referenced resource not found: %s/%s", targetType, targetID), nil, "Reference.reference")
		}
	}
	return nil
}

func localTypedReference(ref types.Reference, self *types.ResourceEnvelope) (resourceType, id string, ok bool) {
	raw := strings.TrimSpace(ref.Raw)
	if raw == "" {
		return "", "", false
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(raw, "#") || strings.HasPrefix(lower, "urn:") ||
		strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return "", "", false
	}
	resourceType = strings.TrimSpace(ref.ResourceType)
	id = strings.TrimSpace(ref.ID)
	if resourceType == "" || id == "" {
		return "", "", false
	}
	if i := strings.Index(id, "/_history/"); i >= 0 {
		id = id[:i]
	}
	if self != nil && resourceType == self.ResourceType && id == self.ID {
		return "", "", false
	}
	return resourceType, id, true
}
