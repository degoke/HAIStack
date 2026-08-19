package http

import (
	"encoding/json"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/types"
)

type formattedResponseWriter struct {
	http.ResponseWriter
	format responseFormat
}

func withResponseFormat(w http.ResponseWriter, format responseFormat) http.ResponseWriter {
	return &formattedResponseWriter{ResponseWriter: w, format: format}
}

func responseFormatFor(w http.ResponseWriter) responseFormat {
	if formatted, ok := w.(*formattedResponseWriter); ok {
		return formatted.format
	}
	return responseFormatJSON
}

func (w *formattedResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func writeOperationOutcome(w http.ResponseWriter, status int, outcome *types.OperationOutcome) {
	if outcome == nil {
		w.WriteHeader(status)
		return
	}
	data, err := json.Marshal(outcome)
	if err != nil {
		w.Header().Set("Content-Type", contentTypeFHIRJSON())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeResource(w, status, data, nil)
}

func writeResource(w http.ResponseWriter, status int, data []byte, headers map[string]string) {
	contentType := contentTypeFHIRJSON()
	if responseFormatFor(w) == responseFormatXML {
		converted, err := marshalFHIRXML(data)
		if err != nil {
			w.Header().Set("Content-Type", contentTypeFHIRJSON())
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(&types.OperationOutcome{
				ResourceType: "OperationOutcome",
				Issue:        []types.OperationIssue{{Severity: "error", Code: "exception", Diagnostics: err.Error()}},
			})
			return
		}
		data = converted
		contentType = contentTypeFHIRXML()
	}
	w.Header().Set("Content-Type", contentType)
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeEnvelope(w http.ResponseWriter, status int, envelope *types.ResourceEnvelope, headers map[string]string) {
	if envelope == nil {
		writeError(w, invalidRequest("service returned no resource", nil))
		return
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if envelope.VersionID != "" {
		headers["ETag"] = `W/"` + envelope.VersionID + `"`
	}
	if !envelope.LastUpdated.IsZero() {
		headers["Last-Modified"] = envelope.LastUpdated.UTC().Format(http.TimeFormat)
	}
	writeResource(w, status, envelope.JSON, headers)
}

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func resourceHeaders(basePath string, envelope *types.ResourceEnvelope) map[string]string {
	headers := map[string]string{}
	if envelope != nil && envelope.ID != "" {
		headers["Location"] = locationURL(basePath, envelope.ResourceType, envelope.ID)
	}
	return headers
}
