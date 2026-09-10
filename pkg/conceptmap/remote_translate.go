package conceptmap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RemoteHTTPClient calls a FHIR server's ConceptMap/$translate operation.
type RemoteHTTPClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Translate performs a remote ConceptMap/$translate request.
func (c RemoteHTTPClient) Translate(ctx context.Context, req TranslateRequest) ([]map[string]any, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("remote terminology base URL is required")
	}
	sourceSystem, sourceCode, sourceDisplay := codingParts(req.Source)
	if sourceCode == "" {
		return nil, fmt.Errorf("translate source coding is required")
	}
	params := map[string]any{
		"resourceType": "Parameters",
		"parameter": []any{
			map[string]any{"name": "url", "valueUri": req.MapCanonical},
		},
	}
	coding := map[string]any{"code": sourceCode}
	if sourceSystem != "" {
		coding["system"] = sourceSystem
	}
	if sourceDisplay != "" {
		coding["display"] = sourceDisplay
	}
	params["parameter"] = append(params["parameter"].([]any), map[string]any{
		"name": "coding", "valueCoding": coding,
	})
	if req.TargetSystem != "" {
		params["parameter"] = append(params["parameter"].([]any), map[string]any{
			"name": "targetsystem", "valueUri": req.TargetSystem,
		})
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/ConceptMap/$translate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/fhir+json")
	httpReq.Header.Set("Accept", "application/fhir+json")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("remote ConceptMap/$translate: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("remote ConceptMap/$translate returned %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}
	return parseTranslateParameters(respBody)
}

func parseTranslateParameters(raw []byte) ([]map[string]any, error) {
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("parse translate response: %w", err)
	}
	parameters, _ := params["parameter"].([]any)
	var matches []map[string]any
	for _, item := range parameters {
		param, _ := item.(map[string]any)
		if param == nil {
			continue
		}
		name, _ := param["name"].(string)
		if name != "match" {
			continue
		}
		part, _ := param["part"].([]any)
		for _, partItem := range part {
			partMap, _ := partItem.(map[string]any)
			if partMap == nil {
				continue
			}
			partName, _ := partMap["name"].(string)
			if partName != "concept" {
				continue
			}
			concept, _ := partMap["valueCoding"].(map[string]any)
			if concept != nil {
				matches = append(matches, concept)
				continue
			}
			nested, _ := partMap["part"].([]any)
			for _, nestedItem := range nested {
				nestedMap, _ := nestedItem.(map[string]any)
				if nestedMap == nil {
					continue
				}
				if nestedName, _ := nestedMap["name"].(string); nestedName != "coding" {
					continue
				}
				if coding, _ := nestedMap["valueCoding"].(map[string]any); coding != nil {
					matches = append(matches, coding)
				}
			}
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("remote ConceptMap/$translate returned no matches")
	}
	return dedupeCodings(matches), nil
}
