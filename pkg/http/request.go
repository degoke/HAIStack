package http

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/types"
)

const maxRequestBodyBytes = 10 << 20 // 10 MiB

func readBody(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
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

func parseResourceBody(codec types.ResourceCodec, resourceType, contentType string, data []byte) (*types.ResourceEnvelope, error) {
	data, actualType, err := requestBodyJSON(contentType, data)
	if err != nil {
		return nil, invalidRequest("parse FHIR body", err)
	}
	envelope, err := codec.ParseJSON(resourceType, data)
	if err != nil {
		return nil, invalidRequest("parse FHIR "+actualType, err)
	}
	return envelope, nil
}

func parseBundleBody(codec types.ResourceCodec, contentType string, data []byte) (*types.ResourceEnvelope, error) {
	data, actualType, err := requestBodyJSON(contentType, data)
	if err != nil {
		return nil, invalidRequest("parse bundle", err)
	}
	envelope, err := codec.ParseJSON("Bundle", data)
	if err != nil {
		return nil, invalidRequest("parse bundle "+actualType, err)
	}
	return envelope, nil
}

func requestBodyJSON(contentType string, data []byte) ([]byte, string, error) {
	mediaType := strings.TrimSpace(contentType)
	if mediaType == "" {
		return data, "JSON", nil
	}
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return nil, "body", err
	}
	switch strings.ToLower(parsed) {
	case "application/fhir+json", "application/json":
		return data, "JSON", nil
	case "application/fhir+xml", "application/xml", "text/xml":
		jsonData, _, err := parseFHIRXML(data)
		return jsonData, "XML", err
	default:
		return nil, "body", fmt.Errorf("unsupported Content-Type %q", contentType)
	}
}
