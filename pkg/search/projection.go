package search

import (
	"encoding/json"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// applyProjection trims resource JSON according to _summary and _elements directives.
func applyProjection(res *types.ResourceEnvelope, summary SummaryMode, elements []string) *types.ResourceEnvelope {
	if res == nil {
		return nil
	}
	if summary == "" && len(elements) == 0 {
		return res
	}
	if summary == SummaryCount {
		return res
	}

	var payload map[string]any
	if err := json.Unmarshal(res.JSON, &payload); err != nil {
		return res
	}

	switch summary {
	case SummaryTrue:
		payload = summaryTrueProjection(payload)
	case SummaryText:
		payload = summaryTextProjection(payload)
	case SummaryData:
		payload = summaryDataProjection(payload)
	}

	if len(elements) > 0 {
		payload = elementsProjection(payload, elements)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return res
	}
	copyEnv := *res
	copyEnv.JSON = raw
	return &copyEnv
}

func summaryTrueProjection(payload map[string]any) map[string]any {
	out := map[string]any{
		"resourceType": payload["resourceType"],
		"id":           payload["id"],
	}
	if meta, ok := payload["meta"].(map[string]any); ok {
		out["meta"] = meta
	}
	return out
}

func summaryTextProjection(payload map[string]any) map[string]any {
	out := summaryTrueProjection(payload)
	if text, ok := payload["text"]; ok {
		out["text"] = text
	}
	return out
}

func summaryDataProjection(payload map[string]any) map[string]any {
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "text" {
			continue
		}
		out[k] = v
	}
	return out
}

func elementsProjection(payload map[string]any, elements []string) map[string]any {
	allowed := make(map[string]struct{})
	for _, el := range elements {
		allowed[el] = struct{}{}
	}
	allowed["resourceType"] = struct{}{}
	allowed["id"] = struct{}{}
	allowed["meta"] = struct{}{}

	out := make(map[string]any)
	for k, v := range payload {
		if _, ok := allowed[k]; ok {
			out[k] = v
		}
	}
	return out
}
