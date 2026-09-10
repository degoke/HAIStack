package structuremap

import "encoding/json"

// Map is the JSON representation of a FHIR StructureMap resource.
type Map struct {
	ResourceType string      `json:"resourceType"`
	URL          string      `json:"url"`
	Version      string      `json:"version,omitempty"`
	Status       string      `json:"status,omitempty"`
	Name         string      `json:"name,omitempty"`
	Structure    []Structure `json:"structure,omitempty"`
	Group        []Group     `json:"group,omitempty"`
}

type Structure struct {
	URL  string `json:"url"`
	Mode string `json:"mode"`
}

type Group struct {
	Name     string  `json:"name"`
	TypeMode string  `json:"typeMode,omitempty"`
	Input    []Input `json:"input,omitempty"`
	Rule     []Rule  `json:"rule,omitempty"`
}

type Input struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type Rule struct {
	Name      string      `json:"name,omitempty"`
	Source    []Source    `json:"source,omitempty"`
	Target    []Target    `json:"target,omitempty"`
	Rule      []Rule      `json:"rule,omitempty"`
	Dependent []Dependent `json:"dependent,omitempty"`
}

type Source struct {
	Context   string   `json:"context"`
	Element   []string `json:"element,omitempty"`
	Variable  string   `json:"variable,omitempty"`
	Condition string   `json:"condition,omitempty"`
	ListMode  string   `json:"listMode,omitempty"`
	Min       int      `json:"min,omitempty"`
	Max       string   `json:"max,omitempty"`
}

type Target struct {
	Context   string      `json:"context"`
	Element   []string    `json:"element,omitempty"`
	Variable  string      `json:"variable,omitempty"`
	Transform string      `json:"transform,omitempty"`
	Parameter []Parameter `json:"parameter,omitempty"`
	ListMode  []string    `json:"listMode,omitempty"`
}

type Parameter struct {
	ValueID      string   `json:"valueId,omitempty"`
	ValueString  string   `json:"valueString,omitempty"`
	ValueBoolean *bool    `json:"valueBoolean,omitempty"`
	ValueInteger *int     `json:"valueInteger,omitempty"`
	ValueDecimal *float64 `json:"valueDecimal,omitempty"`
}

type Dependent struct {
	Name     string   `json:"name"`
	Variable []string `json:"variable,omitempty"`
}

func ParseMap(raw []byte) (Map, error) {
	var m Map
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	if m.ResourceType == "" {
		m.ResourceType = "StructureMap"
	}
	return m, nil
}

func Canonical(m Map) string {
	if m.Version == "" {
		return m.URL
	}
	return m.URL + "|" + m.Version
}
