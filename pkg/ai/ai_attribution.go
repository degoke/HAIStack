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
	// AIAgentContextExtensionURL stores harness/agent correlation on meta.extension (does not replace clinical meta.source).
	AIAgentContextExtensionURL = "urn:haistack:fhir:StructureDefinition:ai-agent-context"
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
	// ProvenanceBestEffort when true (default), a Provenance create failure does not fail the clinical write.
	// Ignored when AtomicProvenance is true (write + Provenance use ProcessTransactionBundle).
	ProvenanceBestEffort *bool
	// AtomicProvenance when true, clinical writes with CreateProvenance use a transaction bundle
	// (resource + Provenance). execute_fhir_transaction is always atomic.
	AtomicProvenance *bool
}

func (c AIAttributionConfig) atomicProvenance() bool {
	if c.AtomicProvenance == nil {
		return false
	}
	return *c.AtomicProvenance
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

func (c AIAttributionConfig) provenanceBestEffort() bool {
	if c.ProvenanceBestEffort == nil {
		return true
	}
	return *c.ProvenanceBestEffort
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
	if conversationID != "" || actor != "" {
		mergeAIAgentContextExtension(meta, conversationID, actor)
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

func mergeAIAgentContextExtension(meta map[string]any, conversationID, actor string) {
	exts, _ := meta["extension"].([]any)
	if exts == nil {
		exts = []any{}
	}
	for _, item := range exts {
		ext, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ext["url"] == AIAgentContextExtensionURL {
			return
		}
	}
	inner := []any{}
	if conversationID != "" {
		inner = append(inner, map[string]any{
			"url":         "conversationId",
			"valueString": conversationID,
		})
	}
	if actor != "" {
		inner = append(inner, map[string]any{
			"url":         "actor",
			"valueString": actor,
		})
	}
	exts = append(exts, map[string]any{
		"url":       AIAgentContextExtensionURL,
		"extension": inner,
	})
	meta["extension"] = exts
}

func hasAIASTCoding(security []any) bool {
	for _, item := range security {
		if codingHasAIAST(item) {
			return true
		}
	}
	return false
}

func codingHasAIAST(item any) bool {
	switch v := item.(type) {
	case map[string]any:
		if codeMatchAIAST(v["system"], v["code"]) {
			return true
		}
		if raw, ok := v["coding"].([]any); ok {
			for _, c := range raw {
				if codingHasAIAST(c) {
					return true
				}
			}
		}
	}
	return false
}

func codeMatchAIAST(system, code any) bool {
	return fmt.Sprint(system) == AIASTCodeSystem && fmt.Sprint(code) == AIASTCode
}

func (e *Executor) stampWriteJSONForAttribution(jsonData []byte, req ToolRequest) ([]byte, error) {
	if !e.cfg.AIAttribution.Enabled {
		return jsonData, nil
	}
	return mergeAIAttributionIntoResourceJSON(jsonData, req.ConversationID, req.Actor)
}

// ProvenanceRecordResult captures best-effort provenance side effects after a write.
type ProvenanceRecordResult struct {
	Err     error
	Warning string
}

func (e *Executor) recordWriteProvenance(ctx context.Context, req ToolRequest, written *types.ResourceEnvelope) ProvenanceRecordResult {
	cfg := e.cfg.AIAttribution
	if !cfg.Enabled || written == nil || !cfg.createProvenance() {
		return ProvenanceRecordResult{}
	}
	err := e.createAIProvenance(ctx, req, written)
	if err == nil {
		return ProvenanceRecordResult{}
	}
	if cfg.provenanceBestEffort() {
		toolName := req.ToolName
		if toolName == "" {
			toolName = ToolCreateFhirResource
		}
		_ = e.logAudit(ctx, req, toolName, "provenance-failed", map[string]string{
			"error":        err.Error(),
			"resourceType": written.ResourceType,
			"id":           written.ID,
		})
		return ProvenanceRecordResult{Err: err, Warning: err.Error()}
	}
	return ProvenanceRecordResult{Err: err}
}

func (e *Executor) createAIProvenance(ctx context.Context, req ToolRequest, written *types.ResourceEnvelope) error {
	if e.cfg.Core == nil {
		return fmt.Errorf("%w: core service required for provenance", ErrMissingDependency)
	}
	body, err := e.marshalAIProvenance(req, written.ResourceType+"/"+written.ID)
	if err != nil {
		return err
	}
	_, err = e.cfg.Core.Create(ctx, &types.ResourceEnvelope{
		ResourceType: "Provenance",
		JSON:         body,
	})
	return err
}

// marshalAIProvenance builds Provenance JSON targeting targetRef (ResourceType/id or urn:uuid:...).
func (e *Executor) marshalAIProvenance(req ToolRequest, targetRef string) ([]byte, error) {
	now := e.cfg.Now()
	if now.IsZero() {
		now = time.Now()
	}
	prov := buildAIProvenanceMap(e.cfg.AIAttribution, req, targetRef, now)
	return json.Marshal(prov)
}

func buildAIProvenanceMap(cfg AIAttributionConfig, req ToolRequest, targetRef string, now time.Time) map[string]any {
	agentWho := map[string]any{}
	display := strings.TrimSpace(cfg.AgentDisplay)
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
			map[string]any{"reference": targetRef},
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
	if model := strings.TrimSpace(cfg.ModelID); model != "" {
		prov["entity"] = []any{
			map[string]any{
				"role": map[string]any{
					"coding": []any{
						map[string]any{
							"system":  "http://terminology.hl7.org/CodeSystem/provenance-entity-role",
							"code":    "source",
							"display": "Source",
						},
					},
				},
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
	return prov
}
