package bulkimport

import "time"

// JobStatus tracks bulk import lifecycle state.
type JobStatus string

const (
	StatusInProgress JobStatus = "in-progress"
	StatusComplete   JobStatus = "complete"
	StatusError      JobStatus = "error"
	StatusCancelled  JobStatus = "cancelled"
)

const InputFormatNDJSON = "application/fhir+ndjson"

// InputFile is one NDJSON source in a kickoff request.
type InputFile struct {
	Type   string `json:"type"`
	URL    string `json:"url,omitempty"`
	NDJSON []byte `json:"-"`
}

// CountFile summarizes imported resources of one type.
type CountFile struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	URL   string `json:"url,omitempty"`
}

// ErrorFile describes one error NDJSON artifact.
type ErrorFile struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// KickoffRequest captures import parameters from an HTTP kickoff.
type KickoffRequest struct {
	TenantID       string
	PrincipalID    string
	InputFormat    string
	Inputs         []InputFile
	RequestURL     string
	RequiresAccess bool
}

// Job is a durable bulk import job record.
type Job struct {
	ID              string
	Status          JobStatus
	Request         KickoffRequest
	TransactionTime time.Time
	Progress        string
	Output          []CountFile
	Errors          []ErrorFile
	CreatedAt       time.Time
	CompletedAt     time.Time
	LastError       string
	CancelRequested bool
}

// Manifest is the completed import manifest returned to clients.
type Manifest struct {
	TransactionTime     time.Time   `json:"transactionTime"`
	Request             string      `json:"request"`
	RequiresAccessToken bool        `json:"requiresAccessToken"`
	Output              []CountFile `json:"output"`
	Error               []ErrorFile `json:"error,omitempty"`
}

// JobPayload is the background job payload for bulk import execution.
type JobPayload struct {
	JobID string `json:"jobId"`
}
