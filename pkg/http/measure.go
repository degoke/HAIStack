package http

import (
	"net/http"
)

func (h *handler) handleEvaluateMeasure(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.MeasureEvaluateService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodGet, http.MethodPost)
		return
	}
	if err := h.authorizeRead(r.Context(), "Measure", route.id); err != nil {
		writeError(w, err)
		return
	}
	var body []byte
	var err error
	if r.Method == http.MethodPost {
		body, err = readBodyAllowEmpty(r)
		if err != nil {
			writeError(w, err)
			return
		}
		if len(body) > 0 {
			body, _, err = requestBodyJSON(r.Header.Get("Content-Type"), body)
			if err != nil {
				writeError(w, invalidRequest("parse $evaluate-measure input", err))
				return
			}
		}
	}
	result, err := h.cfg.MeasureEvaluateService.EvaluateMeasure(r.Context(), MeasureEvaluateRequest{
		ID:    route.id,
		Query: r.URL.Query(),
		Body:  body,
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if result == nil {
		writeError(w, invalidRequest("Measure/$evaluate-measure returned no resource", nil))
		return
	}
	writeEnvelope(w, http.StatusOK, result, nil)
}
