package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/haistack/pkg/types"
)

// HL7 AI Transparency on FHIR: AIAST in Resource.meta.security when AI influenced the resource.
// https://build.fhir.org/ig/HL7/aitransparency-ig/en/requirements.html
const (
	AIASTCodeSystem = "http://terminology.hl7.org/CodeSystem/v3-ObservationValue"
	AIASTCode       = "AIAST"
	AIASTDisplay    = "Artificial Intelligence asserted"
)

// AIAttributionConfig stamps AI transparency metadata on executor writes and optionally records Provenance.
type AIAttributionConfig struct {
	// Enabled applies meta.security (AIAST) and optional Provenance on successful writes.
	Enabled bool
	// CreateProvenance records a Provenance resource targeting the written resource (default true when Enabled).
	CreateProvenance *bool
	// AgentDisplay is used for Provenance.agent[].who.display when set.
	AgentDisplay string
	// ModelID is stored in Provenance.entity or agent extension when set.
	ModelID string
}

func (c AIAttributionConfig) createProvenance() bool {
	if !c.Enabled {
		return false
	}
	if c.CreateProvenance == nil {
		return true
	}
	return *c.CreateProvenance
}

func aiastCoding() map[string]any {
	return map[string]any{
		"system":  AIASTCodeSystem,
		"code":    AIASTCode,
		"display": AIASTDisplay,
	}
}

// mergeAIAttributionIntoResourceJSON adds AIAST to meta.security and optional meta.source for conversation correlation.
func mergeAIAttributionIntoResourceJSON(jsonData []byte, conversationID, actor string) ([]byte, error) {
	var root map[string]any
	if err := json.Unmarshal(jsonData, &root); err != nil {
		return nil, err
	}
	mergeAIASTMeta(root, conversationID, actor)
	return json.Marshal(root)
}

func mergeAIASTMeta(root map[string]any, conversationID, actor string) {
	meta, _ := root["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
		root["meta"] = meta
	}
	if conversationID != "" {
		src := strings.TrimSpace(fmt.Sprintf("urn:haistack:ai:conversation:%s", conversationID))
		if actor != "" {
			src = fmt.Sprintf("%s;actor=%s", src, actor)
		}
		meta["source"] = src
	}
	security, _ := meta["security"].([]any)
	if security == nil {
		security = []any{}
	}
	if !hasAIASTCoding(security) {
		security = append(security, aiastCoding())
	}
	meta["security"] = security
}

func hasAIASTCoding(security []any) bool {
	for _, item := range security {
		coding, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if coding["system"] == AIASTCodeSystem && coding["code"] == AIASTCode {
			return true
		}
	}
	return false
}

func (e *Executor) stampWriteJSONForAttribution(jsonData []byte, req ToolRequest) ([]byte, error) {
	if !e.cfg.AIAttribution.Enabled {
		return jsonData, nil
	}
	return mergeAIAttributionIntoResourceJSON(jsonData, req.ConversationID, req.Actor)
}

func (e *Executor) recordWriteProvenance(ctx context.Context, req ToolRequest, written *types.ResourceEnvelope) error {
	cfg := e.cfg.AIAttribution
	if !cfg.Enabled || written == nil || !cfg.createProvenance() {
		return nil
	}
	return e.createAIProvenance(ctx, req, written)
}

func (e *Executor) createAIProvenance(ctx context.Context, req ToolRequest, written *types.ResourceEnvelope) error {
	if e.cfg.Core == nil {
		return fmt.Errorf("%w: core service required for provenance", ErrMissingDependency)
	}
	now := e.cfg.Now()
	if now.IsZero() {
		now = time.Now()
	}
	agentWho := map[string]any{}
	display := strings.TrimSpace(e.cfg.AIAttribution.AgentDisplay)
	if display == "" && req.Actor != "" {
		display = req.Actor
	}
	if display != "" {
		agentWho["display"] = display
	}
	if req.Actor != "" {
		agentWho["identifier"] = map[string]any{
			"system": "urn:haistack:ai:actor",
			"value":  req.Actor,
		}
	}
	prov := map[string]any{
		"resourceType": "Provenance",
		"target": []any{
			map[string]any{"reference": written.ResourceType + "/" + written.ID},
		},
		"recorded": now.UTC().Format(time.RFC3339),
		"agent": []any{
			map[string]any{
				"type": map[string]any{
					"coding": []any{
						map[string]any{
							"system":  "http://terminology.hl7.org/CodeSystem/provenance-participant-type",
							"code":    "author",
							"display": "Author",
						},
					},
				},
				"who": agentWho,
			},
		},
		"reason": []any{aiastCoding()},
	}
	if model := strings.TrimSpace(e.cfg.AIAttribution.ModelID); model != "" {
		prov["entity"] = []any{
			map[string]any{
				"role": "source",
				"what": map[string]any{
					"identifier": map[string]any{
						"system": "urn:haistack:ai:model",
						"value":  model,
					},
				},
			},
		}
	}
	if req.ConversationID != "" {
		prov["activity"] = map[string]any{
			"coding": []any{
				map[string]any{
					"system":  "urn:haistack:ai:activity",
					"code":    "conversation",
					"display": req.ConversationID,
				},
			},
		}
	}
	body, err := json.Marshal(prov)
	if err != nil {
		return err
	}
	_, err = e.cfg.Core.Create(ctx, &types.ResourceEnvelope{
		ResourceType: "Provenance",
		JSON:         body,
	})
	return err
}
