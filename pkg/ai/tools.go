package ai

// Write operation constants for harness drafts and policy.
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

// GenericToolDescriptors returns metadata for the six built-in generic tools.
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
			Name:        ToolCreateFhirResource,
			Description: "Create a FHIR resource using structured top-level fields (not full Resource JSON)",
			Generic:     true,
			InputKeys:   []string{"resourceType", "id", "fields"},
		},
		{
			Name:        ToolUpdateFhirResource,
			Description: "Update a FHIR resource using patch-path keys (FHIR Patch path syntax, e.g. name[0].family—not FHIRPath functions like .where()) mapped to values in patches",
			Generic:     true,
			InputKeys:   []string{"resourceType", "id", "patches"},
		},
		{
			Name:        ToolExecuteFhirTransaction,
			Description: "Execute a FHIR bundle: bundleType transaction (atomic, default) or batch (independent entries). Entries: POST fields, PUT patches, batch GET read. Provenance POSTs are appended when AI attribution is on (atomic only with transaction).",
			Generic:     true,
			InputKeys:   []string{"bundleType", "entries"},
		},
	}
}

// IsWriteTool reports whether name is a FHIR write tool (create, update, or transaction).
func IsWriteTool(name string) bool {
	switch name {
	case ToolCreateFhirResource, ToolUpdateFhirResource, ToolExecuteFhirTransaction:
		return true
	default:
		return false
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
