package sdc

import (
	"encoding/json"
)

func applyObservationMetadata(entries []map[string]any, q Questionnaire) []map[string]any {
	if q.ObservationLinkPeriod == nil {
		return entries
	}
	period := map[string]any{}
	if q.ObservationLinkPeriod.Start != "" {
		period["start"] = q.ObservationLinkPeriod.Start
	}
	if q.ObservationLinkPeriod.End != "" {
		period["end"] = q.ObservationLinkPeriod.End
	}
	if len(period) == 0 {
		return entries
	}
	for i, entry := range entries {
		resource, asRaw, ok := observationResourceFromEntry(entry)
		if !ok {
			continue
		}
		resource["effectivePeriod"] = period
		entry["resource"] = marshalObservationResource(resource, asRaw)
		entries[i] = entry
	}
	return entries
}

func observationResourceFromEntry(entry map[string]any) (map[string]any, bool, bool) {
	switch raw := entry["resource"].(type) {
	case json.RawMessage:
		var resource map[string]any
		if json.Unmarshal(raw, &resource) != nil {
			return nil, false, false
		}
		if resource["resourceType"] != "Observation" {
			return nil, false, false
		}
		return resource, true, true
	case map[string]any:
		if raw["resourceType"] != "Observation" {
			return nil, false, false
		}
		return raw, false, true
	default:
		return nil, false, false
	}
}

func marshalObservationResource(resource map[string]any, asRaw bool) any {
	if !asRaw {
		return resource
	}
	b, err := json.Marshal(resource)
	if err != nil {
		return resource
	}
	return json.RawMessage(b)
}
