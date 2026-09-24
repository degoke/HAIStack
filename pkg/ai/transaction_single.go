package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (e *Executor) execSingleWriteViaAtomicTransaction(ctx context.Context, req ToolRequest, params persistWriteParams) (any, []Citation, string, bool, string, []string, error) {
	data, citations, outcome, approval, token, redactions, err := e.execPersistWriteViaTransaction(ctx, req, params)
	if err != nil || outcome != "success" {
		return data, citations, outcome, approval, token, redactions, err
	}
	out := map[string]any{
		"operation":    params.Operation,
		"resourceType": params.ResourceType,
		"atomic":       true,
	}
	if params.ID != "" {
		out["id"] = params.ID
	} else if m, ok := data.(map[string]any); ok {
		if raw, ok := m["response"]; ok {
			if id := firstTransactionLocationID(raw); id != "" {
				out["id"] = id
			}
		}
	}
	citations = []Citation{e.cfg.Citations.WriteCitation(params.Operation, params.ResourceType, fmt.Sprint(out["id"]))}
	return out, citations, "success", false, "", nil, nil
}

func firstTransactionLocationID(response any) string {
	obj, ok := response.(map[string]any)
	if !ok {
		return ""
	}
	entries, ok := obj["entry"].([]any)
	if !ok {
		return ""
	}
	for _, item := range entries {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resp, ok := entry["response"].(map[string]any)
		if !ok {
			continue
		}
		loc, _ := resp["location"].(string)
		if id := parseFHIRLocationID(loc); id != "" {
			return id
		}
	}
	return ""
}

func parseFHIRLocationID(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if i := strings.Index(location, "/"); i >= 0 {
		location = location[i+1:]
	}
	if j := strings.LastIndex(location, "/"); j >= 0 {
		return location[j+1:]
	}
	return location
}

func (e *Executor) execPersistWriteViaTransaction(ctx context.Context, req ToolRequest, params persistWriteParams) (any, []Citation, string, bool, string, []string, error) {
	spec := transactionEntrySpec{
		ResourceType: params.ResourceType,
		ID:           params.ID,
	}
	switch params.Operation {
	case WriteOperationCreate:
		spec.Method = "POST"
		spec.Fields = params.Allowed
	case WriteOperationUpdate:
		spec.Method = "PUT"
		spec.ID = params.ID
		spec.Patches = params.Allowed
	default:
		return nil, nil, "", false, "", nil, fmt.Errorf("%w: unknown operation %q", ErrInvalidInput, params.Operation)
	}
	data, citations, outcome, approval, token, redactions, err := e.execBundleSpecs(ctx, req, "transaction", []transactionEntrySpec{spec}, false)
	if err != nil {
		return nil, nil, "", false, "", nil, err
	}
	if m, ok := data.(map[string]any); ok && m["response"] != nil {
		return m, citations, outcome, approval, token, redactions, nil
	}
	// Re-marshal if needed
	if b, err := json.Marshal(data); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			return m, citations, outcome, approval, token, redactions, nil
		}
	}
	return data, citations, outcome, approval, token, redactions, nil
}
