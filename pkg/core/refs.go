package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

type pendingIdentitiesKey struct{}

// pendingIdentities records Type/id and urn:uuid values that will exist after
// the current transaction bundle commits, including entries not yet persisted.
// resolved maps matching urn:uuid fullUrl values to the target entry Type/id
// used to rewrite intra-bundle references before persist.
type pendingIdentities struct {
	typed    map[string]struct{}
	urns     map[string]struct{}
	resolved map[string]string
}

func newPendingIdentities() *pendingIdentities {
	return &pendingIdentities{
		typed:    make(map[string]struct{}),
		urns:     make(map[string]struct{}),
		resolved: make(map[string]string),
	}
}

func (p *pendingIdentities) addTyped(resourceType, id string) {
	if p == nil {
		return
	}
	resourceType = strings.TrimSpace(resourceType)
	id = strings.TrimSpace(id)
	if resourceType == "" || id == "" {
		return
	}
	p.typed[resourceType+"/"+id] = struct{}{}
}

func (p *pendingIdentities) addURN(raw string) {
	if p == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), "urn:uuid:") {
		return
	}
	p.urns[strings.ToLower(raw)] = struct{}{}
}

func (p *pendingIdentities) hasTyped(resourceType, id string) bool {
	if p == nil {
		return false
	}
	_, ok := p.typed[resourceType+"/"+id]
	return ok
}

func (p *pendingIdentities) hasURN(raw string) bool {
	if p == nil {
		return false
	}
	_, ok := p.urns[strings.ToLower(strings.TrimSpace(raw))]
	return ok
}

func (p *pendingIdentities) resolveURN(raw, typed string) {
	if p == nil {
		return
	}
	raw = strings.TrimSpace(raw)
	typed = strings.TrimSpace(typed)
	if typed == "" || !strings.HasPrefix(strings.ToLower(raw), "urn:uuid:") {
		return
	}
	p.resolved[strings.ToLower(raw)] = typed
}

func (p *pendingIdentities) resolvedTypeID(raw string) (string, bool) {
	if p == nil {
		return "", false
	}
	typed, ok := p.resolved[strings.ToLower(strings.TrimSpace(raw))]
	return typed, ok
}

func (p *pendingIdentities) hasResolvedURNs() bool {
	return p != nil && len(p.resolved) > 0
}

func rewriteReferenceStrings(data []byte, pending *pendingIdentities) ([]byte, error) {
	if pending == nil || !pending.hasResolvedURNs() {
		return data, nil
	}
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	rewriteReferenceValue(v, pending)
	return json.Marshal(v)
}

func rewriteReferenceValue(v interface{}, pending *pendingIdentities) {
	switch val := v.(type) {
	case map[string]interface{}:
		if raw, ok := val["reference"].(string); ok {
			if typed, ok := pending.resolvedTypeID(raw); ok {
				val["reference"] = typed
			}
		}
		for k, child := range val {
			if k == "reference" {
				continue
			}
			rewriteReferenceValue(child, pending)
		}
	case []interface{}:
		for _, item := range val {
			rewriteReferenceValue(item, pending)
		}
	}
}

func contextWithPendingIdentities(ctx context.Context, pending *pendingIdentities) context.Context {
	if pending == nil {
		return ctx
	}
	return context.WithValue(ctx, pendingIdentitiesKey{}, pending)
}

func pendingIdentitiesFromContext(ctx context.Context) *pendingIdentities {
	pending, _ := ctx.Value(pendingIdentitiesKey{}).(*pendingIdentities)
	return pending
}

// checkReferentialIntegrity requires local typed relative references to exist
// before persist. Contained fragments, absolute URLs, untyped ids, and
// self-references are skipped. Unresolved URNs are skipped; urn:uuid values
// that match a transaction-bundle fullUrl are treated as present (normally
// those refs are rewritten to Type/id before this check). Resources written
// earlier in the same write session and identities collected from the current
// transaction bundle (including later-or-earlier entries) satisfy Exists.
func (s *ResourceService) checkReferentialIntegrity(ctx context.Context, session store.WriteSession, envelope *types.ResourceEnvelope) error {
	if !s.enforceReferentialIntegrity {
		return nil
	}
	if envelope == nil {
		return nil
	}
	refs, err := types.GetReferences(envelope.JSON)
	if err != nil {
		return invalidErr("parse resource references", err)
	}
	pending := pendingIdentitiesFromContext(ctx)
	resources := session.ResourceStore()
	for _, ref := range refs {
		if pending.hasURN(ref.Raw) {
			continue
		}
		targetType, targetID, ok := localTypedReference(ref, envelope)
		if !ok {
			continue
		}
		if pending.hasTyped(targetType, targetID) {
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
