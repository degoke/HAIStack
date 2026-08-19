package http

import (
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func (h *handler) handleSDCOperation(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.SDCService == nil {
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}
	if err := h.authorizeWrite(r.Context(), strings.TrimPrefix(route.operation, "$"), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	body, err := readBody(r)
	if err != nil {
		writeError(w, err)
		return
	}
	body, _, err = requestBodyJSON(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeError(w, invalidRequest("parse SDC operation input", err))
		return
	}
	var input *types.ResourceEnvelope
	if len(body) > 0 {
		input, err = h.cfg.Codec.ParseJSON("", body)
		if err != nil {
			writeError(w, invalidRequest("parse SDC operation input", err))
			return
		}
	}
	req := SDCRequest{Body: input, Parameters: input, Query: r.URL.Query()}
	if route.resourceType == "Questionnaire" && route.id != "" {
		env, e := h.cfg.ResourceService.Read(r.Context(), "Questionnaire", route.id)
		if e != nil {
			writeError(w, e)
			return
		}
		req.Questionnaire = env
	}
	if route.resourceType == "QuestionnaireResponse" && route.id != "" {
		env, e := h.cfg.ResourceService.Read(r.Context(), "QuestionnaireResponse", route.id)
		if e != nil {
			writeError(w, e)
			return
		}
		req.QuestionnaireResponse = env
	}
	var result *types.ResourceEnvelope
	switch route.operation {
	case "$populate":
		result, err = h.cfg.SDCService.Populate(r.Context(), req)
	case "$validate":
		var outcome *types.OperationOutcome
		outcome, err = h.cfg.SDCService.Validate(r.Context(), req)
		if err == nil {
			writeOperationOutcome(w, http.StatusOK, outcome)
			return
		}
	case "$extract":
		result, err = h.cfg.SDCService.Extract(r.Context(), req)
	case "$assemble":
		result, err = h.cfg.SDCService.Assemble(r.Context(), req)
	case "$next-question", "$next", "$answer":
		result, err = h.cfg.SDCService.Adaptive(r.Context(), route.operation, req)
	default:
		writeError(w, unsupportedEndpoint(r.URL.Path))
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	if result == nil {
		writeError(w, invalidRequest("SDC operation returned no resource", nil))
		return
	}
	writeEnvelope(w, http.StatusOK, result, nil)
}
