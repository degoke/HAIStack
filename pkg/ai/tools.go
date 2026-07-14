package ai

// Write operation constants for write_fhir_resource input.
const (
	WriteOperationCreate = "create"
	WriteOperationUpdate = "update"
)

// ToolDescriptor describes one tool for model discovery and operator review.
type ToolDescriptor struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Generic     bool     `json:"generic"`
	Delegate    string   `json:"delegate,omitempty"`
	InputKeys   []string `json:"inputKeys,omitempty"`
}

// GenericToolDescriptors returns metadata for the four built-in generic tools.
func GenericToolDescriptors() []ToolDescriptor {
	return []ToolDescriptor{
		{
			Name:        ToolReadFhirResource,
			Description: "Read one FHIR resource by type and id through policy allow-lists",
			Generic:     true,
			InputKeys:   []string{"resourceType", "id"},
		},
		{
			Name:        ToolSearchFhirResources,
			Description: "Search FHIR resources with allow-listed parameters and bounded paging",
			Generic:     true,
			InputKeys:   []string{"resourceType", "params", "count", "offset"},
		},
		{
			Name:        ToolRunView,
			Description: "Execute a registered ViewDefinition and return structured rows",
			Generic:     true,
			InputKeys:   []string{"viewName", "version", "parameters", "limit", "offset"},
		},
		{
			Name:        ToolWriteFhirResource,
			Description: "Create or update a resource using structured field-level input",
			Generic:     true,
			InputKeys:   []string{"operation", "resourceType", "id", "fields"},
		},
	}
}

// AllToolDescriptors returns generic tools plus registered convenience wrappers.
func (r *Registry) AllToolDescriptors() []ToolDescriptor {
	descriptors := GenericToolDescriptors()
	if r == nil {
		return descriptors
	}
	for _, spec := range r.List() {
		descriptors = append(descriptors, ToolDescriptor{
			Name:        spec.Name,
			Description: spec.Description,
			Generic:     false,
			Delegate:    spec.Delegate,
		})
	}
	return descriptors
}
