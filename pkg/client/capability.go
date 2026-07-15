package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CapabilityMetadata holds parsed CapabilityStatement fields.
type CapabilityMetadata struct {
	FHIRVersion string
	Formats     []string
	Resources   []ResourceCapability
	Raw         []byte
}

// ResourceCapability describes one supported resource type.
type ResourceCapability struct {
	Type         string
	Interactions []string
	SearchParams []string
}

// Metadata fetches the server CapabilityStatement.
func (c *Client) Metadata(ctx context.Context) (*CapabilityMetadata, error) {
	raw, err := c.do(ctx, requestOptions{
		method: "GET",
		url:    c.fhirURL("metadata"),
	})
	if err != nil {
		return nil, err
	}
	return parseCapability(raw.Body)
}

// CheckFHIRVersion returns the server's advertised FHIR version, or the client default.
func (c *Client) CheckFHIRVersion(ctx context.Context) (string, error) {
	meta, err := c.Metadata(ctx)
	if err != nil {
		return c.fhirVersion, err
	}
	if meta.FHIRVersion != "" {
		return meta.FHIRVersion, nil
	}
	return c.fhirVersion, nil
}

// CheckFeatureSupport reports whether the server advertises a resource interaction.
func (c *Client) CheckFeatureSupport(ctx context.Context, resourceType, interaction string) (bool, error) {
	meta, err := c.Metadata(ctx)
	if err != nil {
		return false, err
	}
	for _, res := range meta.Resources {
		if res.Type != resourceType {
			continue
		}
		for _, i := range res.Interactions {
			if i == interaction {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

func parseCapability(data []byte) (*CapabilityMetadata, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, err
	}
	rt, _ := obj["resourceType"].(string)
	if rt != "CapabilityStatement" {
		return nil, fmt.Errorf("expected CapabilityStatement, got %q", rt)
	}
	meta := &CapabilityMetadata{
		Raw: append([]byte(nil), data...),
	}
	if v, ok := obj["fhirVersion"].(string); ok {
		meta.FHIRVersion = v
	}
	if formats, ok := obj["format"].([]interface{}); ok {
		for _, f := range formats {
			if s, ok := f.(string); ok {
				meta.Formats = append(meta.Formats, s)
			}
		}
	}
	rest, ok := obj["rest"].([]interface{})
	if !ok || len(rest) == 0 {
		return meta, nil
	}
	first, ok := rest[0].(map[string]interface{})
	if !ok {
		return meta, nil
	}
	resources, ok := first["resource"].([]interface{})
	if !ok {
		return meta, nil
	}
	for _, r := range resources {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		rc := ResourceCapability{}
		rc.Type, _ = rm["type"].(string)
		if interactions, ok := rm["interaction"].([]interface{}); ok {
			for _, i := range interactions {
				im, ok := i.(map[string]interface{})
				if !ok {
					continue
				}
				code, _ := im["code"].(string)
				if code != "" {
					rc.Interactions = append(rc.Interactions, code)
				}
			}
		}
		if params, ok := rm["searchParam"].([]interface{}); ok {
			for _, p := range params {
				pm, ok := p.(map[string]interface{})
				if !ok {
					continue
				}
				name, _ := pm["name"].(string)
				if name != "" {
					rc.SearchParams = append(rc.SearchParams, name)
				}
			}
		}
		meta.Resources = append(meta.Resources, rc)
	}
	return meta, nil
}

// NormalizeFHIRVersion maps common version strings to a comparable form.
func NormalizeFHIRVersion(version string) string {
	v := strings.TrimSpace(version)
	switch {
	case strings.HasPrefix(v, "4."):
		return "R4"
	case strings.HasPrefix(v, "3."):
		return "STU3"
	case strings.HasPrefix(v, "5."):
		return "R5"
	default:
		return v
	}
}
