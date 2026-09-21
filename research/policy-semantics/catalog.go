package policysemantics

import (
	"encoding/json"
	"fmt"
	"os"
)

// DefaultCatalog returns the published scenario catalogue.
func DefaultCatalog() ([]Scenario, error) {
	return ParseCatalog(catalogJSON)
}

// Scenario is one vendor-neutral authorization case: principal + scopes +
// consent + request → expected decision.
type Scenario struct {
	ID            string        `json:"id"`
	Doc           string        `json:"doc"`
	Principal     string        `json:"principal"`
	Policy        string        `json:"policy"`
	Scopes        string        `json:"scopes,omitempty"`
	LaunchPatient string        `json:"launchPatient,omitempty"`
	PatientScope  string        `json:"patientScope,omitempty"`
	Tenant        string        `json:"tenant,omitempty"`
	PurposeOfUse  string        `json:"purposeOfUse,omitempty"`
	Action        string        `json:"action"`
	ResourceType  string        `json:"resourceType,omitempty"`
	ResourceID    string        `json:"resourceId,omitempty"`
	ToolName      string        `json:"toolName,omitempty"`
	Consent       *ConsentState `json:"consent,omitempty"`
	ExpectAllow   bool          `json:"expectAllow"`
}

// ConsentState is a portable R4 Consent / future Permission projection.
type ConsentState struct {
	Status        string   `json:"status"`
	PatientID     string   `json:"patientId,omitempty"`
	ProvisionType string   `json:"provisionType"`
	ResourceTypes []string `json:"resourceTypes,omitempty"`
	Actions       []string `json:"actions,omitempty"`
}

// CatalogFile is the published JSON envelope.
type CatalogFile struct {
	Version   string     `json:"version"`
	Scenarios []Scenario `json:"scenarios"`
}

// LoadCatalog reads a scenarios JSON file.
func LoadCatalog(path string) ([]Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseCatalog(raw)
}

// ParseCatalog unmarshals the published catalogue.
func ParseCatalog(raw []byte) ([]Scenario, error) {
	var file CatalogFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, err
	}
	if len(file.Scenarios) == 0 {
		return nil, fmt.Errorf("policy-semantics: catalogue is empty")
	}
	seen := map[string]struct{}{}
	for _, sc := range file.Scenarios {
		if sc.ID == "" {
			return nil, fmt.Errorf("policy-semantics: scenario missing id")
		}
		if _, ok := seen[sc.ID]; ok {
			return nil, fmt.Errorf("policy-semantics: duplicate id %q", sc.ID)
		}
		seen[sc.ID] = struct{}{}
	}
	return file.Scenarios, nil
}
