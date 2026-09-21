package benchmarks

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Workload is a portable, implementation-agnostic operation definition.
type Workload struct {
	Name         string `json:"name" yaml:"name"`
	Description  string `json:"description" yaml:"description"`
	Operation    string `json:"operation" yaml:"operation"`
	ResourceType string `json:"resourceType,omitempty" yaml:"resourceType,omitempty"`
	ViewName     string `json:"viewName,omitempty" yaml:"viewName,omitempty"`
	ViewVersion  string `json:"viewVersion,omitempty" yaml:"viewVersion,omitempty"`
	Optional     bool   `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// WorkloadFile is the published YAML envelope.
type WorkloadFile struct {
	Name        string     `json:"name" yaml:"name"`
	Version     string     `json:"version" yaml:"version"`
	Description string     `json:"description" yaml:"description"`
	Workloads   []Workload `json:"workloads" yaml:"workloads"`
}

// LoadWorkloads reads a YAML or JSON workload file.
func LoadWorkloads(path string) (WorkloadFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return WorkloadFile{}, err
	}
	var file WorkloadFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		if jerr := json.Unmarshal(raw, &file); jerr != nil {
			return WorkloadFile{}, fmt.Errorf("parse workloads: yaml: %v; json: %v", err, jerr)
		}
	}
	if len(file.Workloads) == 0 {
		return WorkloadFile{}, fmt.Errorf("benchmarks: no workloads in %s", path)
	}
	return file, nil
}
