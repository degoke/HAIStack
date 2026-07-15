package http

import (
	"io"
	"net/http"

	"github.com/degoke/health-ai-stack/pkg/types"
)

const maxRequestBodyBytes = 10 << 20 // 10 MiB

func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	limited := io.LimitReader(r.Body, maxRequestBodyBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, invalidRequest("read request body", err)
	}
	if int64(len(data)) > maxRequestBodyBytes {
		return nil, invalidRequest("request body too large", nil)
	}
	if len(data) == 0 {
		return nil, invalidRequest("request body is required", nil)
	}
	return data, nil
}

func parseResourceBody(codec types.ResourceCodec, resourceType string, data []byte) (*types.ResourceEnvelope, error) {
	envelope, err := codec.ParseJSON(resourceType, data)
	if err != nil {
		return nil, invalidRequest("parse FHIR JSON", err)
	}
	return envelope, nil
}

func parseBundleBody(codec types.ResourceCodec, data []byte) (*types.ResourceEnvelope, error) {
	envelope, err := codec.ParseJSON("Bundle", data)
	if err != nil {
		return nil, invalidRequest("parse bundle JSON", err)
	}
	return envelope, nil
}
