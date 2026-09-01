package http

import (
	"io"
	"net/http"
)

func (h *handler) handleValidateOperation(w http.ResponseWriter, r *http.Request, route parsedRoute) {
	if h.cfg.ValidateService == nil {
		writeError(w, notImplementedEndpoint(r.URL.Path))
		return
	}
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, r.Method, http.MethodPost)
		return
	}
	if err := h.authorizeRead(r.Context(), route.resourceType, route.id); err != nil {
		writeError(w, err)
		return
	}
	body, err := readBodyAllowEmpty(r)
	if err != nil {
		writeError(w, err)
		return
	}
	outcome, err := h.cfg.ValidateService.Validate(r.Context(), ValidateRequest{
		ResourceType: route.resourceType,
		ID:           route.id,
		ContentType:  r.Header.Get("Content-Type"),
		Body:         body,
		Query:        r.URL.Query(),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeOperationOutcome(w, http.StatusOK, outcome)
}

func readBodyAllowEmpty(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	limited := io.LimitReader(r.Body, maxRequestBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, invalidRequest("read request body", err)
	}
	if int64(len(data)) > maxRequestBodyBytes {
		return nil, invalidRequest("request body too large", nil)
	}
	return data, nil
}

func isSDCResourceOperation(operation, resourceType string) bool {
	switch resourceType {
	case "Questionnaire", "QuestionnaireResponse":
		switch operation {
		case "$populate", "$validate", "$extract", "$assemble", "$next-question", "$next", "$answer":
			return true
		}
	}
	return false
}
