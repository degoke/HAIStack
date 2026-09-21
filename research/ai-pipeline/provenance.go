package main

import (
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/store"
)

// ProvenanceBundle is the exportable FAIR artefact for one pipeline run.
type ProvenanceBundle struct {
	Artefact   string               `json:"artefact"`
	Track      string               `json:"track"`
	CreatedAt  time.Time            `json:"createdAt"`
	FAIR       FAIRMetadata         `json:"fair"`
	Inputs     []InputRecord        `json:"inputs"`
	Validation ValidationProvenance `json:"validation"`
	View       ViewProvenance       `json:"view"`
	Policy     PolicyProvenance     `json:"policy"`
	Tool       ToolProvenance       `json:"tool"`
	Model      ModelProvenance      `json:"model"`
	Output     OutputProvenance     `json:"output"`
	Audit      []store.AuditRecord  `json:"audit"`
}

// FAIRMetadata records license and reuse constraints.
type FAIRMetadata struct {
	License   string `json:"license"`
	Synthetic bool   `json:"synthetic"`
	PHI       bool   `json:"phi"`
	Citation  string `json:"citation"`
}

// ValidationProvenance pins the FHIR/IG versions and the profiles actually
// applied to pipeline inputs. IGPackage/IGVersion come from conformance-lock.json;
// Mode and Profiles record what the validator ran, not only the lock citation.
type ValidationProvenance struct {
	FHIRVersion string   `json:"fhirVersion"`
	IGPackage   string   `json:"igPackage"`
	IGVersion   string   `json:"igVersion"`
	Canonical   string   `json:"canonical,omitempty"`
	GitCommit   string   `json:"conformanceLockCommit,omitempty"`
	Mode        string   `json:"mode"`
	Profiles    []string `json:"profiles,omitempty"`
	IGResources string   `json:"igResources,omitempty"`
}

// InputRecord is one hashed FHIR resource that entered the pipeline.
type InputRecord struct {
	ResourceType string `json:"resourceType"`
	ID           string `json:"id"`
	Hash         string `json:"hash"`
	Validated    bool   `json:"validated"`
	Profile      string `json:"profile,omitempty"`
}

// ViewProvenance identifies the ViewDefinition and its row set.
type ViewProvenance struct {
	Name       string   `json:"name"`
	Version    string   `json:"version"`
	Definition string   `json:"definitionHash"`
	RowCount   int      `json:"rowCount"`
	RowHash    string   `json:"rowHash"`
	Columns    []string `json:"columns"`
}

// PolicyProvenance identifies the policy document used for permissioning.
type PolicyProvenance struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
	Format  string `json:"format"`
}

// ToolProvenance identifies the AI tool invocation.
type ToolProvenance struct {
	Name      string        `json:"name"`
	Actor     string        `json:"actor"`
	Outcome   string        `json:"outcome"`
	Citations []ai.Citation `json:"citations,omitempty"`
}

// ModelProvenance identifies the stub model.
type ModelProvenance struct {
	Adapter string `json:"adapter"`
	Seed    int64  `json:"seed"`
}

// OutputProvenance is the model-facing result.
type OutputProvenance struct {
	Content string `json:"content"`
	Context string `json:"contextHash"`
}
