package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	clinicalQuantityInText = regexp.MustCompile(`(?i)\b(\d+(?:\.\d+)?)\s*(mg/dL|mmol/L|mmHg|bpm|kg|g|mL|U/L|mcg|µg|IU/L|%)\b`)
	plainNumberInText      = regexp.MustCompile(`\b(\d{2,}(?:\.\d+)?)\b`)
	searchParamNameInText  = regexp.MustCompile(`(?i)\b(?:search(?:ed)?|filter(?:ed)?|query)\s+(?:for|by|on)\s+([a-z_][a-z0-9_-]*)\s*=`)
)

// toolCallRecord pairs a harness tool summary with the parsed tool input (for search param grounding).
type toolCallRecord struct {
	summary HarnessToolResult
	input   map[string]any
}

func toolCallRecords(toolSummaries []HarnessToolResult, inputs []map[string]any) []toolCallRecord {
	out := make([]toolCallRecord, len(toolSummaries))
	for i, tr := range toolSummaries {
		var input map[string]any
		if i < len(inputs) {
			input = inputs[i]
		}
		out[i] = toolCallRecord{summary: tr, input: input}
	}
	return out
}

// AnalyzeClinicalValueGrounding flags numeric clinical quantities in the answer that do not appear in tool JSON evidence.
func AnalyzeClinicalValueGrounding(answer string, toolSummaries []HarnessToolResult) []string {
	evidence := toolEvidenceBlob(toolSummaries)
	if strings.TrimSpace(evidence) == "" {
		return nil
	}
	evidenceLower := strings.ToLower(evidence)
	var warnings []string
	seen := map[string]bool{}
	for _, match := range clinicalQuantityInText.FindAllStringSubmatch(answer, -1) {
		if len(match) < 3 {
			continue
		}
		token := strings.ToLower(strings.TrimSpace(match[1] + " " + match[2]))
		if seen[token] {
			continue
		}
		seen[token] = true
		if !strings.Contains(evidenceLower, strings.ToLower(match[1])) {
			warnings = append(warnings, fmt.Sprintf("answer mentions clinical value %q not found in tool results", strings.TrimSpace(match[0])))
		}
	}
	if looksClinicalNumericAnswer(answer) {
		for _, match := range plainNumberInText.FindAllStringSubmatch(answer, -1) {
			if len(match) < 2 {
				continue
			}
			num := match[1]
			if seen[num] {
				continue
			}
			seen[num] = true
			if strings.Contains(evidenceLower, num) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf("answer mentions numeric value %q not found in tool results", num))
		}
	}
	return warnings
}

func looksClinicalNumericAnswer(answer string) bool {
	m := strings.ToLower(answer)
	clues := []string{"lab", "glucose", "a1c", "hemoglobin", "bp", "blood pressure", "heart rate", "temperature", "weight", "bmi", "creatinine", "potassium", "sodium", "mg/dl", "mmhg"}
	for _, c := range clues {
		if strings.Contains(m, c) {
			return true
		}
	}
	return false
}

func toolEvidenceBlob(toolSummaries []HarnessToolResult) string {
	var parts []string
	for _, tr := range toolSummaries {
		if tr.Err != nil || tr.Result == nil || tr.Result.Data == nil {
			continue
		}
		switch tr.ToolName {
		case ToolReadFhirResource, ToolSearchFhirResources, ToolRunView, ToolGetPatientSummary, ToolGetUpcomingAppointments, ToolSearchPatientByPhone:
		default:
			continue
		}
		b, err := json.Marshal(tr.Result.Data)
		if err != nil {
			continue
		}
		parts = append(parts, string(b))
		if tr.Result.Context != "" {
			parts = append(parts, tr.Result.Context)
		}
	}
	return strings.Join(parts, "\n")
}

// AnalyzeSearchParamGrounding warns when the answer cites FHIR search parameter names not used in search tool calls this turn.
func AnalyzeSearchParamGrounding(answer string, records []toolCallRecord) []string {
	used := map[string]bool{}
	for _, rec := range records {
		if rec.summary.ToolName != ToolSearchFhirResources || rec.summary.Err != nil {
			continue
		}
		for k := range SearchParamsFromToolInput(rec.input) {
			used[k] = true
		}
	}
	if len(used) == 0 {
		return nil
	}
	var warnings []string
	for token := range extractMentionedSearchParamNames(answer) {
		if !used[token] {
			warnings = append(warnings, fmt.Sprintf("answer mentions search parameter %q not used in search_fhir_resources calls this turn", token))
		}
	}
	for _, match := range searchParamNameInText.FindAllStringSubmatch(answer, -1) {
		if len(match) < 2 {
			continue
		}
		name := strings.ToLower(match[1])
		if !used[name] {
			warnings = append(warnings, fmt.Sprintf("answer references search parameter %q not present in search_fhir_resources calls", name))
		}
	}
	return warnings
}

func extractMentionedSearchParamNames(answer string) map[string]bool {
	out := map[string]bool{}
	lower := strings.ToLower(answer)
	for _, name := range []string{"name", "family", "given", "birthdate", "gender", "identifier", "telecom", "patient", "code", "date", "status"} {
		if strings.Contains(lower, name+" parameter") || strings.Contains(lower, name+" search") || strings.Contains(lower, "search "+name) {
			out[name] = true
		}
	}
	return out
}

// SearchParamsFromToolInput extracts normalized search parameter base names from tool input.
func SearchParamsFromToolInput(input map[string]any) map[string]bool {
	out := map[string]bool{}
	if input == nil {
		return out
	}
	raw, ok := input["params"]
	if !ok || raw == nil {
		return out
	}
	params, err := parseStringSliceMap(raw)
	if err != nil {
		return out
	}
	for key := range params {
		base := paramBaseName(key)
		if base != "" && base != "_count" && base != "_offset" {
			out[strings.ToLower(base)] = true
		}
	}
	return out
}

// PreflightSearchPolicy validates search tool input against policy before execution (stricter harness path).
func PreflightSearchPolicy(ctx context.Context, policy PolicyEngine, req ToolRequest, input map[string]any) error {
	if policy == nil {
		return nil
	}
	parsed, err := parseSearchInput(input)
	if err != nil {
		return err
	}
	params := url.Values{}
	for k, vals := range parsed.Params {
		for _, v := range vals {
			params.Add(k, v)
		}
	}
	decision, err := policy.CheckSearch(ctx, SearchPolicyRequest{
		Actor: req.Actor, Subject: req.Subject,
		ResourceType: parsed.ResourceType, Params: params,
	})
	if err != nil {
		return err
	}
	if decision == nil || !decision.Allowed {
		return fmt.Errorf("%w: search %s", ErrPolicyDenied, parsed.ResourceType)
	}
	return nil
}
