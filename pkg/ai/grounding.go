package ai

import (
	"fmt"
	"regexp"
	"strings"
)

// GroundingMode controls harness anti-hallucination behavior for FHIR-backed answers.
type GroundingMode string

const (
	// GroundingOff disables harness grounding prompts and post-checks (executor policy still applies).
	GroundingOff GroundingMode = "off"
	// GroundingStandard adds instructions and records warnings on the ChatResult.
	GroundingStandard GroundingMode = "standard"
	// GroundingStrict rejects answers that lack tool evidence or cite unknown resource refs.
	GroundingStrict GroundingMode = "strict"
)

// GroundingConfig tunes grounding for natural-language ↔ FHIR workflows.
type GroundingConfig struct {
	Mode GroundingMode
	// AppendFHIRInstructions adds default grounding text to the model system prompt (default true when Mode != off).
	AppendFHIRInstructions bool
	// RequireToolEvidenceOnDataQuestions requires a successful read/search/view tool with citations before answering data-style questions (strict).
	RequireToolEvidenceOnDataQuestions bool
	// RejectUncitedResourceRefs fails the turn when the answer mentions Resource/id literals not present in citations (strict).
	RejectUncitedResourceRefs bool
	// RejectUncitedClinicalValues fails when the answer states clinical numbers/units not present in tool JSON (strict).
	RejectUncitedClinicalValues bool
	// ValidateSearchParamsInAnswer warns when the answer cites search parameters not used in search tool calls.
	ValidateSearchParamsInAnswer bool
	// PreflightSearchPolicy runs policy.CheckSearch before search_fhir_resources executes (strict default).
	PreflightSearchPolicy bool
}

// DefaultFHIRGroundingSystemPrompt instructs models to stay on the executor tool path.
const DefaultFHIRGroundingSystemPrompt = `You are a clinical agent backed by a FHIR server. Ground every factual claim in tool results from this turn or earlier tool messages in the thread.

Rules:
- For patient/chart facts (demographics, labs, meds, diagnoses, appointments): call read_fhir_resource, search_fhir_resources, or view_fhir before stating them.
- Never invent resource ids, patient names, or clinical values. If tools return no data, say so explicitly.
- When stating facts from FHIR, reference the resource (e.g. Patient/abc) matching tool citations.
- Writes and updates should use propose_write_resource or propose_write_plan, then host CommitWrite / CommitWritePlan (or policy approval flows).
- View definitions and search filters must come from authorized tools; do not fabricate search parameters.
- Summaries and narratives for people or downstream models must reflect tool output only.`

var fhirResourceRefInText = regexp.MustCompile(`\b([A-Z][A-Za-z]+)/([A-Za-z0-9\-\.]+)\b`)

// AppendGroundingSystemPrompt merges host system prompt with FHIR grounding instructions.
func AppendGroundingSystemPrompt(systemPrompt string, cfg GroundingConfig) string {
	if cfg.Mode == GroundingOff {
		return systemPrompt
	}
	if !cfg.appendInstructions() {
		return systemPrompt
	}
	base := strings.TrimSpace(systemPrompt)
	grounding := strings.TrimSpace(DefaultFHIRGroundingSystemPrompt)
	if base == "" {
		return grounding
	}
	return base + "\n\n" + grounding
}

func (c GroundingConfig) appendInstructions() bool {
	if c.Mode == GroundingOff {
		return false
	}
	if c.AppendFHIRInstructions {
		return true
	}
	// Default on for standard/strict when field left false.
	return c.Mode == GroundingStandard || c.Mode == GroundingStrict
}

func (c GroundingConfig) effectiveStrict() GroundingConfig {
	if c.Mode != GroundingStrict {
		return c
	}
	out := c
	if !out.RequireToolEvidenceOnDataQuestions {
		out.RequireToolEvidenceOnDataQuestions = true
	}
	if !out.RejectUncitedResourceRefs {
		out.RejectUncitedResourceRefs = true
	}
	if !out.RejectUncitedClinicalValues {
		out.RejectUncitedClinicalValues = true
	}
	if !out.ValidateSearchParamsInAnswer {
		out.ValidateSearchParamsInAnswer = true
	}
	if !out.PreflightSearchPolicy {
		out.PreflightSearchPolicy = true
	}
	return out
}

// AnalyzeAnswerGrounding returns warnings when answer text cites resources not backed by citations.
func AnalyzeAnswerGrounding(answer string, citations []Citation) []string {
	allowed := citationRefSet(citations)
	var warnings []string
	for _, match := range fhirResourceRefInText.FindAllStringSubmatch(answer, -1) {
		if len(match) < 3 {
			continue
		}
		ref := match[1] + "/" + match[2]
		if skipGroundingRef(ref) {
			continue
		}
		if !allowed[ref] {
			warnings = append(warnings, fmt.Sprintf("answer mentions %q without a matching tool citation", ref))
		}
	}
	return warnings
}

func citationRefSet(citations []Citation) map[string]bool {
	out := map[string]bool{}
	for _, c := range citations {
		if c.Ref != "" {
			out[c.Ref] = true
		}
	}
	return out
}

func skipGroundingRef(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "http/"),
		strings.HasPrefix(ref, "https/"),
		ref == "application/json":
		return true
	default:
		return false
	}
}

func looksLikeFHIRDataQuestion(message string) bool {
	m := strings.ToLower(strings.TrimSpace(message))
	if m == "" {
		return false
	}
	keywords := []string{
		"patient", "fhir", "resource", "observation", "condition", "medication",
		"encounter", "diagnosis", "lab", "result", "chart", "record", "who is",
		"find", "search", "list", "show me", "read", "summary", "view",
	}
	for _, kw := range keywords {
		if strings.Contains(m, kw) {
			return true
		}
	}
	return false
}

func hasFHIREvidence(toolSummaries []HarnessToolResult) bool {
	for _, tr := range toolSummaries {
		if tr.Err != nil || tr.Result == nil {
			continue
		}
		switch tr.ToolName {
		case ToolReadFhirResource, ToolSearchFhirResources, ToolRunView, ToolGetPatientSummary, ToolGetUpcomingAppointments, ToolSearchPatientByPhone:
		default:
			continue
		}
		if len(tr.Result.Citations) > 0 {
			return true
		}
	}
	return false
}

func (h *Harness) groundingConfig() GroundingConfig {
	if h == nil {
		return GroundingConfig{Mode: GroundingStandard}
	}
	cfg := h.cfg.Grounding
	if cfg.Mode == "" {
		cfg.Mode = GroundingStandard
	}
	return cfg.effectiveStrict()
}

func (h *Harness) effectiveSystemPrompt(tools []ChatTool) string {
	base := h.systemPromptForModel(tools)
	return AppendGroundingSystemPrompt(base, h.groundingConfig())
}

func (h *Harness) finalizeChatResult(invocationID, userMessage, answer string, toolSummaries []HarnessToolResult, toolInputs []map[string]any) (*ChatResult, error) {
	res := h.buildChatResult(invocationID, answer, toolSummaries)
	gcfg := h.groundingConfig()
	if gcfg.Mode == GroundingOff {
		return res, nil
	}
	records := toolCallRecords(toolSummaries, toolInputs)
	res.GroundingWarnings = AnalyzeAnswerGrounding(answer, res.Citations)
	if gcfg.ValidateSearchParamsInAnswer {
		res.GroundingWarnings = append(res.GroundingWarnings, AnalyzeSearchParamGrounding(answer, records)...)
	}
	if gcfg.RejectUncitedClinicalValues || gcfg.Mode == GroundingStrict {
		res.GroundingWarnings = append(res.GroundingWarnings, AnalyzeClinicalValueGrounding(answer, toolSummaries)...)
	}
	if gcfg.RequireToolEvidenceOnDataQuestions && looksLikeFHIRDataQuestion(userMessage) && !hasFHIREvidence(toolSummaries) {
		msg := "FHIR data question answered without successful read/search/view tool evidence"
		res.GroundingWarnings = append(res.GroundingWarnings, msg)
	}
	if gcfg.Mode == GroundingStrict && len(res.GroundingWarnings) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrUngroundedAnswer, strings.Join(res.GroundingWarnings, "; "))
	}
	return res, nil
}
