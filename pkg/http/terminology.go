package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func (h *handler) handleTerminologyOperation(w http.ResponseWriter, r *http.Request, route parsedRoute) bool {
	if h.cfg.TerminologyService == nil {
		return false
	}
	switch route.operation {
	case "$lookup":
		if route.resourceType != "CodeSystem" {
			return false
		}
		h.handleCodeSystemLookup(w, r, route)
		return true
	case "$expand":
		if route.resourceType != "ValueSet" {
			return false
		}
		h.handleValueSetExpand(w, r, route)
		return true
	case "$validate-code":
		switch route.resourceType {
		case "CodeSystem", "ValueSet":
			h.handleValidateCode(w, r, route)
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (h *handler) handleCodeSystemLookup(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	system, version, code := lookupParams(r)
	if system == "" || code == "" {
		writeError(w, invalidRequest("system and code are required for $lookup", nil))
		return
	}
	ctx, err := h.withTerminologyInstalls(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := h.cfg.TerminologyService.Lookup(ctx, terminology.LookupRequest{
		ScopeID: h.terminologyScope(ctx),
		System:  system,
		Version: version,
		Code:    code,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, lookupParameters(result), nil)
}

func (h *handler) handleValueSetExpand(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	url, version, offset, count := expandParams(r)
	if url == "" {
		writeError(w, invalidRequest("url is required for $expand", nil))
		return
	}
	ctx, err := h.withTerminologyInstalls(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	expansion, err := h.cfg.TerminologyService.Expand(ctx, terminology.ExpandRequest{
		ScopeID: h.terminologyScope(ctx),
		URL:     url,
		Version: version,
		Offset:  offset,
		Count:   count,
	})
	if err != nil {
		if errors.Is(err, terminology.ErrExpansionTooCostly) {
			writeOperationOutcome(w, http.StatusRequestEntityTooLarge, &types.OperationOutcome{
				ResourceType: "OperationOutcome",
				Issue: []types.OperationIssue{{
					Severity:    "error",
					Code:        "too-costly",
					Diagnostics: err.Error(),
				}},
			})
			return
		}
		if errors.Is(err, terminology.ErrExpansionNotFound) {
			writeError(w, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: err.Error()})
			return
		}
		writeError(w, err)
		return
	}
	if expansion == nil {
		writeError(w, &core.ServiceError{Kind: core.ErrorKindNotFound, Message: "ValueSet not found"})
		return
	}
	writeEnvelope(w, http.StatusOK, expansionValueSet(url, version, expansion), nil)
}

func (h *handler) handleValidateCode(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	ctx, err := h.withTerminologyInstalls(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	req, err := validateCodeRequest(r, route.resourceType, h.terminologyScope(ctx))
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := h.cfg.TerminologyService.ValidateCode(ctx, req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeEnvelope(w, http.StatusOK, validateCodeParameters(result), nil)
}

func lookupParams(r *http.Request) (system, version, code string) {
	q := r.URL.Query()
	system = strings.TrimSpace(q.Get("system"))
	version = strings.TrimSpace(q.Get("version"))
	code = strings.TrimSpace(q.Get("code"))
	if system != "" && code != "" {
		return system, version, code
	}
	body, err := readBodyAllowEmpty(r)
	if err != nil || len(body) == 0 {
		return system, version, code
	}
	var params map[string]any
	if json.Unmarshal(body, &params) != nil {
		return system, version, code
	}
	for _, p := range parameterList(params) {
		name, _ := p["name"].(string)
		switch name {
		case "system":
			system = parameterString(p, "valueUri", "valueUrl", "valueString")
		case "version":
			version = parameterString(p, "valueString")
		case "code":
			code = parameterString(p, "valueCode", "valueString")
		}
	}
	return system, version, code
}

func expandParams(r *http.Request) (url, version string, offset, count int) {
	q := r.URL.Query()
	url = strings.TrimSpace(q.Get("url"))
	version = strings.TrimSpace(q.Get("version"))
	offset = atoiDefault(q.Get("offset"), 0)
	count = atoiDefault(q.Get("count"), 0)
	if url != "" {
		return url, version, offset, count
	}
	body, err := readBodyAllowEmpty(r)
	if err != nil || len(body) == 0 {
		return url, version, offset, count
	}
	var params map[string]any
	if json.Unmarshal(body, &params) != nil {
		return url, version, offset, count
	}
	for _, p := range parameterList(params) {
		name, _ := p["name"].(string)
		switch name {
		case "url":
			url = parameterString(p, "valueUri", "valueUrl", "valueString")
		case "version":
			version = parameterString(p, "valueString")
		case "offset":
			offset = atoiDefault(parameterString(p, "valueInteger", "valueString"), 0)
		case "count":
			count = atoiDefault(parameterString(p, "valueInteger", "valueString"), 0)
		}
	}
	return url, version, offset, count
}

func validateCodeRequest(r *http.Request, resourceType, scope string) (terminology.ValidateCodeRequest, error) {
	q := r.URL.Query()
	req := terminology.ValidateCodeRequest{ScopeID: scope}
	if resourceType == "ValueSet" {
		req.URL = strings.TrimSpace(q.Get("url"))
		req.Version = strings.TrimSpace(q.Get("valueSetVersion"))
		if req.Version == "" {
			req.Version = strings.TrimSpace(q.Get("version"))
		}
	}
	coding := terminology.Coding{
		System:  strings.TrimSpace(q.Get("system")),
		Version: strings.TrimSpace(q.Get("version")),
		Code:    strings.TrimSpace(q.Get("code")),
		Display: strings.TrimSpace(q.Get("display")),
	}
	if coding.System != "" && coding.Code != "" {
		req.Coding = coding
		return req, nil
	}
	body, err := readBodyAllowEmpty(r)
	if err != nil {
		return req, err
	}
	if len(body) == 0 {
		if req.Coding.System == "" || req.Coding.Code == "" {
			return req, invalidRequest("coding system and code are required for $validate-code", nil)
		}
		return req, nil
	}
	var params map[string]any
	if err := json.Unmarshal(body, &params); err != nil {
		return req, invalidRequest("parse $validate-code input", err)
	}
	for _, p := range parameterList(params) {
		name, _ := p["name"].(string)
		switch name {
		case "url":
			req.URL = parameterString(p, "valueUri", "valueUrl", "valueString")
		case "valueSetVersion":
			if resourceType == "ValueSet" {
				req.Version = parameterString(p, "valueString")
			}
		case "coding":
			if part, ok := p["part"].([]any); ok {
				for _, pv := range part {
					pm, _ := pv.(map[string]any)
					switch pm["name"] {
					case "system":
						req.Coding.System = parameterString(pm, "valueUri", "valueUrl", "valueString")
					case "version":
						req.Coding.Version = parameterString(pm, "valueString")
					case "code":
						req.Coding.Code = parameterString(pm, "valueCode", "valueString")
					case "display":
						req.Coding.Display = parameterString(pm, "valueString")
					}
				}
			}
		case "code":
			req.Coding.Code = parameterString(p, "valueCode", "valueString")
		case "system":
			req.Coding.System = parameterString(p, "valueUri", "valueUrl", "valueString")
		case "version":
			if resourceType == "ValueSet" && req.URL != "" {
				req.Version = parameterString(p, "valueString")
			} else {
				req.Coding.Version = parameterString(p, "valueString")
			}
		case "display":
			req.Coding.Display = parameterString(p, "valueString")
		}
	}
	if req.Coding.System == "" || req.Coding.Code == "" {
		return req, invalidRequest("coding system and code are required for $validate-code", nil)
	}
	return req, nil
}

func lookupParameters(result *terminology.LookupResult) *types.ResourceEnvelope {
	params := []map[string]any{{"name": "result", "valueBoolean": result != nil && result.Found}}
	if result != nil && result.Found {
		params = append(params,
			map[string]any{"name": "display", "valueString": result.Concept.Display},
			map[string]any{"name": "definition", "valueString": result.Concept.Definition},
			map[string]any{"name": "version", "valueString": result.Concept.Version},
		)
	}
	return parametersEnvelope(params)
}

func expansionValueSet(url, version string, expansion *terminology.Expansion) *types.ResourceEnvelope {
	if expansion == nil {
		return nil
	}
	contains := make([]map[string]any, 0, len(expansion.Contains))
	for _, c := range expansion.Contains {
		contains = append(contains, map[string]any{
			"system":  c.System,
			"version": c.Version,
			"code":    c.Code,
			"display": c.Display,
		})
	}
	raw, _ := json.Marshal(map[string]any{
		"resourceType": "ValueSet",
		"url":          url,
		"version":      version,
		"expansion": map[string]any{
			"total":    expansion.Total,
			"contains": contains,
		},
	})
	return &types.ResourceEnvelope{ResourceType: "ValueSet", JSON: raw}
}

func validateCodeParameters(result *terminology.ValidationResult) *types.ResourceEnvelope {
	params := []map[string]any{{"name": "result", "valueBoolean": result != nil && result.Status == terminology.Valid}}
	if result != nil && result.Message != "" {
		params = append(params, map[string]any{"name": "message", "valueString": result.Message})
	}
	return parametersEnvelope(params)
}

func parametersEnvelope(params []map[string]any) *types.ResourceEnvelope {
	raw, _ := json.Marshal(map[string]any{
		"resourceType": "Parameters",
		"parameter":    params,
	})
	return &types.ResourceEnvelope{ResourceType: "Parameters", JSON: raw}
}

func parameterList(params map[string]any) []map[string]any {
	raw, _ := params["parameter"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func parameterString(p map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := p[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func atoiDefault(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return v
}

// TerminologyService is the terminology provider surface exposed over HTTP.
type TerminologyService = terminology.Service
