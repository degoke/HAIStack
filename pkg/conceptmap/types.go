package conceptmap

import "encoding/json"

// Map is the JSON representation of a FHIR ConceptMap resource.
type Map struct {
	ResourceType string  `json:"resourceType"`
	URL          string  `json:"url"`
	Version      string  `json:"version,omitempty"`
	Status       string  `json:"status,omitempty"`
	Name         string  `json:"name,omitempty"`
	Group        []Group `json:"group,omitempty"`
}

type Group struct {
	Source  string    `json:"source,omitempty"`
	Target  string    `json:"target,omitempty"`
	Element []Element `json:"element,omitempty"`
	Unmapped *Unmapped `json:"unmapped,omitempty"`
}

type Element struct {
	Code    string   `json:"code,omitempty"`
	Display string   `json:"display,omitempty"`
	NoMap   bool     `json:"noMap,omitempty"`
	Target  []Target `json:"target,omitempty"`
}

type Target struct {
	Code         string `json:"code,omitempty"`
	Display      string `json:"display,omitempty"`
	Equivalence  string `json:"equivalence,omitempty"`
	Relationship string `json:"relationship,omitempty"`
}

type Unmapped struct {
	Mode    string `json:"mode,omitempty"`
	Code    string `json:"code,omitempty"`
	Display string `json:"display,omitempty"`
}

func ParseMap(raw []byte) (Map, error) {
	var m Map
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	if m.ResourceType == "" {
		m.ResourceType = "ConceptMap"
	}
	return m, nil
}

func Canonical(m Map) string {
	if m.Version == "" {
		return m.URL
	}
	return m.URL + "|" + m.Version
}
