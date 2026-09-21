package aipipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/audit"
)

// InputRef is one hashed FHIR input in the provenance chain.
type InputRef struct {
	Ref  string `json:"ref"`
	Hash string `json:"hash"`
}

// ViewProvenance identifies the ViewDefinition that produced rows.
type ViewProvenance struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	RowCount int    `json:"rowCount"`
}

// PolicyProvenance identifies the policy document.
type PolicyProvenance struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

// ToolProvenance identifies the AI tool invocation.
type ToolProvenance struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
}

// ModelProvenance identifies the stub (or other adapter).
type ModelProvenance struct {
	Adapter string `json:"adapter"`
	Seed    int64  `json:"seed"`
	Content string `json:"content,omitempty"`
}

// AuditEvent is a compact audit record copied from pkg/audit.
type AuditEvent struct {
	ID       string            `json:"id,omitempty"`
	Action   string            `json:"action"`
	Outcome  string            `json:"outcome"`
	ToolName string            `json:"toolName,omitempty"`
	ViewName string            `json:"viewName,omitempty"`
	Actor    string            `json:"actor,omitempty"`
	Details  map[string]string `json:"details,omitempty"`
}

// FAIR is dataset-level metadata for the pipeline artefact.
type FAIR struct {
	License     string `json:"license"`
	Synthetic   bool   `json:"synthetic"`
	ContainsPHI bool   `json:"containsPhi"`
	Citation    string `json:"citation"`
}

// ProvenanceBundle is the exportable chain from FHIR → view → tool → audit.
type ProvenanceBundle struct {
	Pipeline    string           `json:"pipeline"`
	Version     string           `json:"version"`
	GeneratedAt time.Time        `json:"generatedAt"`
	FAIR        FAIR             `json:"fair"`
	Inputs      []InputRef       `json:"inputs"`
	View        ViewProvenance   `json:"view"`
	Policy      PolicyProvenance `json:"policy"`
	Tool        ToolProvenance   `json:"tool"`
	Model       ModelProvenance  `json:"model"`
	Citations   []ai.Citation    `json:"citations"`
	Output      string           `json:"output"`
	Audit       []AuditEvent     `json:"audit"`
}

func hashBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func compactAudit(events []audit.Event) []AuditEvent {
	out := make([]AuditEvent, 0, len(events))
	for _, ev := range events {
		out = append(out, AuditEvent{
			ID:       ev.ID,
			Action:   ev.Action,
			Outcome:  ev.Outcome,
			ToolName: ev.ToolName,
			ViewName: ev.ViewName,
			Actor:    ev.Actor,
			Details:  ev.Details,
		})
	}
	return out
}

func policyHash(raw []byte) string {
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return hashBytes(raw)
	}
	canonical, err := json.Marshal(obj)
	if err != nil {
		return hashBytes(raw)
	}
	return hashBytes(canonical)
}
