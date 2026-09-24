package ai

import (
	"fmt"
	"strings"
)

// HarnessExecutorGuardrails returns production wiring reminders for Executor used behind Harness.
// Policy and de-identification rules remain application-specific; this helper only checks
// cross-cutting agent safety seams discussed for the harness layer.
func HarnessExecutorGuardrails(cfg Config) []string {
	var notes []string
	if cfg.Policy == nil {
		notes = append(notes, "Config.Policy is required for Executor")
	}
	if !cfg.AuditRequired {
		notes = append(notes, "set Config.AuditRequired and Config.Audit in production agent deployments")
	} else if cfg.Audit == nil {
		notes = append(notes, "Config.AuditRequired is true but Config.Audit is nil")
	}
	if cfg.RequireConversationID {
		notes = append(notes, "set HarnessConfig.AutoConversationID or Harness.SetConversationID before Chat when RequireConversationID is enabled")
	}
	if cfg.Deidentify == nil {
		notes = append(notes, "configure Config.Deidentify when policy enables Deidentify on read/search/view tools")
	}
	if !cfg.RequireValidatorOnWrites {
		notes = append(notes, "set Config.RequireValidatorOnWrites and Config.Validator so all create/update commits are validated")
	} else if cfg.Validator == nil {
		notes = append(notes, "Config.RequireValidatorOnWrites is true but Config.Validator is nil")
	}
	return notes
}

// HarnessGroundingGuardrails returns wiring notes for FHIR grounding behind Harness.
func HarnessGroundingGuardrails(hcfg HarnessConfig) []string {
	var notes []string
	mode := hcfg.Grounding.Mode
	if mode == "" {
		mode = GroundingStandard
	}
	if mode == GroundingStrict && !hcfg.BlockDirectWriteTools {
		notes = append(notes, "GroundingStrict: enable BlockDirectWriteTools so writes go through propose_write_resource / propose_write_plan and CommitWrite / CommitWritePlan")
	}
	if mode != GroundingOff && hcfg.ToolContextFormat != ToolContextMarkdown {
		notes = append(notes, "set ToolContextFormat to markdown so tool rows are easier for models to quote accurately")
	}
	if hcfg.RequireCommitConfirmation && hcfg.CommitWritePlanConfirm == nil {
		notes = append(notes, "RequireCommitConfirmation is true but CommitWritePlanConfirm is nil (all FHIR write harness paths will fail)")
	}
	if hcfg.Executor != nil && hcfg.Executor.Policy() == nil {
		notes = append(notes, "Executor policy is required for PreflightSearchPolicy during Chat")
	}
	return notes
}

// FormatHarnessExecutorGuardrails renders guardrail notes as bullet lines (empty when OK).
func FormatHarnessExecutorGuardrails(cfg Config) string {
	notes := HarnessExecutorGuardrails(cfg)
	if len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	for _, note := range notes {
		b.WriteString("- ")
		b.WriteString(note)
		b.WriteByte('\n')
	}
	return b.String()
}

// FilterToolDescriptorsForHarness optionally removes direct write tools from the model tool list.
func FilterToolDescriptorsForHarness(descriptors []ToolDescriptor, blockDirectWrites bool) []ToolDescriptor {
	if !blockDirectWrites {
		return descriptors
	}
	out := make([]ToolDescriptor, 0, len(descriptors))
	for _, d := range descriptors {
		if IsWriteTool(d.Name) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// MergeHarnessCitations appends unique citations from tool results (stable order).
func MergeHarnessCitations(existing []Citation, results []HarnessToolResult) []Citation {
	merged := append([]Citation(nil), existing...)
	seen := map[string]struct{}{}
	for _, c := range merged {
		seen[citationKey(c)] = struct{}{}
	}
	for _, tr := range results {
		if tr.Result == nil {
			continue
		}
		for _, c := range tr.Result.Citations {
			key := citationKey(c)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, c)
		}
	}
	return merged
}

func citationKey(c Citation) string {
	if c.Ref != "" {
		return c.Kind + ":" + c.Ref
	}
	return fmt.Sprintf("%s:%v", c.Kind, c.Detail)
}
