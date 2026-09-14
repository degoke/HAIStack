package export

import (
	"time"
)

// JobStatus tracks bulk export lifecycle state.
type JobStatus string

const (
	StatusInProgress JobStatus = "in-progress"
	StatusComplete   JobStatus = "complete"
	StatusError      JobStatus = "error"
	StatusCancelled  JobStatus = "cancelled"
)

// OutputFile describes one exported NDJSON artifact.
type OutputFile struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ErrorFile describes one error NDJSON artifact.
type ErrorFile struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// KickoffRequest captures export parameters from an HTTP kickoff.
type KickoffRequest struct {
	TenantID       string
	PrincipalID    string
	ResourceTypes  []string
	Since          time.Time
	GroupID        string
	TypeFilter     string
	OutputFormat   string
	RequestURL     string
	RequiresAccess bool
}

// Job is a durable bulk export job record.
type Job struct {
	ID              string
	Status          JobStatus
	Request         KickoffRequest
	TransactionTime time.Time
	Progress        string
	Output          []OutputFile
	Errors          []ErrorFile
	CreatedAt       time.Time
	CompletedAt     time.Time
	LastError       string
	CancelRequested bool
}

// Manifest is the completed export manifest returned to clients.
type Manifest struct {
	TransactionTime     time.Time    `json:"transactionTime"`
	Request             string       `json:"request"`
	RequiresAccessToken bool         `json:"requiresAccessToken"`
	Output              []OutputFile `json:"output"`
	Error               []ErrorFile  `json:"error,omitempty"`
}

// JobPayload is the background job payload for bulk export execution.
type JobPayload struct {
	JobID string `json:"jobId"`
}
