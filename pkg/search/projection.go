package search

import (
	"encoding/json"
	"fmt"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// applyProjection trims resource JSON according to _summary and _elements directives.
func applyProjection(res *types.ResourceEnvelope, summary SummaryMode, elements []string) (*types.ResourceEnvelope, error) {
	if res == nil {
		return nil, nil
	}
	if summary == "" && len(elements) == 0 {
		return res, nil
	}
	if summary == SummaryCount {
		return res, nil
	}

	var payload map[string]any
	if err := json.Unmarshal(res.JSON, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode %s/%s: %v", ErrProjectionFailed, res.ResourceType, res.ID, err)
	}
	original := payload

	switch summary {
	case SummaryTrue:
		payload = summaryTrueProjection(payload)
	case SummaryText:
		payload = summaryTextProjection(payload)
	case SummaryData:
		payload = summaryDataProjection(payload)
	}

	if len(elements) > 0 {
		for key, value := range elementsProjection(original, elements) {
			payload[key] = value
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode %s/%s: %v", ErrProjectionFailed, res.ResourceType, res.ID, err)
	}
	copyEnv := *res
	copyEnv.JSON = raw
	return &copyEnv, nil
}

// ProjectResource applies the same _summary/_elements projection used by
// search results to a read response.
func ProjectResource(res *types.ResourceEnvelope, summary SummaryMode, elements []string) (*types.ResourceEnvelope, error) {
	return applyProjection(res, summary, elements)
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
