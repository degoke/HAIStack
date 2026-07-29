package http

import (
	"encoding/json"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/types"
)

func writeOperationOutcome(w http.ResponseWriter, status int, outcome *types.OperationOutcome) {
	w.Header().Set("Content-Type", contentTypeFHIRJSON())
	w.WriteHeader(status)
	if outcome == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(outcome)
}

func writeResource(w http.ResponseWriter, status int, data []byte, headers map[string]string) {
	w.Header().Set("Content-Type", contentTypeFHIRJSON())
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeEnvelope(w http.ResponseWriter, status int, envelope *types.ResourceEnvelope, headers map[string]string) {
	if headers == nil {
		headers = map[string]string{}
	}
	if envelope != nil {
		if envelope.VersionID != "" {
			headers["ETag"] = `W/"` + envelope.VersionID + `"`
		}
		if !envelope.LastUpdated.IsZero() {
			headers["Last-Modified"] = envelope.LastUpdated.UTC().Format(http.TimeFormat)
		}
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
