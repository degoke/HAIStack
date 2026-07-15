package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BulkExportClient supports standard FHIR bulk export patterns.
type BulkExportClient struct {
	client *Client
}

// ExportJobStatus tracks bulk export job state.
type ExportJobStatus string

const (
	ExportStatusInProgress ExportJobStatus = "in-progress"
	ExportStatusComplete   ExportJobStatus = "complete"
	ExportStatusError      ExportJobStatus = "error"
	ExportStatusCancelled  ExportJobStatus = "cancelled"
)

// ExportKickoffRequest configures a bulk export kickoff.
type ExportKickoffRequest struct {
	ResourceTypes []string
	Since         time.Time
	GroupID       string
	OutputFormat  string
	TypeFilter    string
}

// ExportJob represents an in-flight or completed bulk export job.
type ExportJob struct {
	StatusURL    string
	Status       ExportJobStatus
	Progress     string
	TransactionTime time.Time
	Request      string
	RequiresAccessToken bool
	Raw          []byte
}

// ExportManifest is the parsed export manifest.
type ExportManifest struct {
	TransactionTime time.Time          `json:"transactionTime"`
	Request         string             `json:"request"`
	RequiresAccessToken bool           `json:"requiresAccessToken"`
	Output          []ExportOutputFile `json:"output"`
	Error           []ExportErrorFile  `json:"error,omitempty"`
	Raw             []byte             `json:"-"`
}

// ExportOutputFile describes one exported NDJSON file.
type ExportOutputFile struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// ExportErrorFile describes one error NDJSON file.
type ExportErrorFile struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// Kickoff starts a system-level or group bulk export.
func (b *BulkExportClient) Kickoff(ctx context.Context, req ExportKickoffRequest) (*ExportJob, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("bulk export client is nil")
	}
	var u string
	if req.GroupID != "" {
		u = b.client.fhirURL("Group", req.GroupID, "$export")
	} else {
		u = b.client.fhirURL("$export")
	}
	values := url.Values{}
	if len(req.ResourceTypes) > 0 {
		values.Set("_type", strings.Join(req.ResourceTypes, ","))
	}
	if !req.Since.IsZero() {
		values.Set("_since", req.Since.UTC().Format(time.RFC3339))
	}
	if req.OutputFormat != "" {
		values.Set("_outputFormat", req.OutputFormat)
	}
	if req.TypeFilter != "" {
		values.Set("_typeFilter", req.TypeFilter)
	}
	if encoded := values.Encode(); encoded != "" {
		u += "?" + encoded
	}
	raw, err := b.client.do(ctx, requestOptions{
		method: "GET",
		url:    u,
		headers: map[string]string{
			"Prefer": "respond-async",
		},
	})
	if err != nil {
		return nil, err
	}
	if raw.StatusCode != 202 {
		return nil, parseError(raw.StatusCode, raw.Body, defaultRetryable(raw.StatusCode))
	}
	statusURL := raw.Header.Get("Content-Location")
	if statusURL == "" {
		return nil, fmt.Errorf("missing Content-Location header")
	}
	return &ExportJob{
		StatusURL: statusURL,
		Status:    ExportStatusInProgress,
		Raw:       append([]byte(nil), raw.Body...),
	}, nil
}

// PollStatus checks the status of a bulk export job.
func (b *BulkExportClient) PollStatus(ctx context.Context, statusURL string) (*ExportJob, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("bulk export client is nil")
	}
	raw, err := b.client.do(ctx, requestOptions{
		method: "GET",
		url:    statusURL,
		accept: "application/json",
	})
	if err != nil {
		return nil, err
	}
	job := &ExportJob{
		StatusURL: statusURL,
		Raw:       append([]byte(nil), raw.Body...),
	}
	if raw.StatusCode == http.StatusOK {
		job.Status = ExportStatusComplete
		return job, nil
	}
	if raw.StatusCode == http.StatusAccepted {
		job.Status = ExportStatusInProgress
		if p := raw.Header.Get("X-Progress"); p != "" {
			job.Progress = p
		}
		return job, nil
	}
	return nil, parseError(raw.StatusCode, raw.Body, false)
}

// Wait polls until the export reaches a terminal state or the context is cancelled.
func (b *BulkExportClient) Wait(ctx context.Context, statusURL string, interval time.Duration) (*ExportJob, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		job, err := b.PollStatus(ctx, statusURL)
		if err != nil {
			return nil, err
		}
		switch job.Status {
		case ExportStatusComplete:
			return job, nil
		case ExportStatusError, ExportStatusCancelled:
			return job, fmt.Errorf("export job ended with status %s", job.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// Cancel cancels an in-progress bulk export job.
func (b *BulkExportClient) Cancel(ctx context.Context, statusURL string) error {
	if b == nil || b.client == nil {
		return fmt.Errorf("bulk export client is nil")
	}
	raw, err := b.client.do(ctx, requestOptions{
		method: "DELETE",
		url:    statusURL,
	})
	if err != nil {
		return err
	}
	if raw.StatusCode == 202 || raw.StatusCode == 204 {
		return nil
	}
	return parseError(raw.StatusCode, raw.Body, false)
}

// GetManifest retrieves and parses the export manifest from a completed job status URL.
func (b *BulkExportClient) GetManifest(ctx context.Context, statusURL string) (*ExportManifest, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("bulk export client is nil")
	}
	raw, err := b.client.do(ctx, requestOptions{
		method: "GET",
		url:    statusURL,
		accept: "application/json",
	})
	if err != nil {
		return nil, err
	}
	if raw.StatusCode != http.StatusOK {
		return nil, parseError(raw.StatusCode, raw.Body, false)
	}
	var manifest ExportManifest
	if err := json.Unmarshal(raw.Body, &manifest); err != nil {
		return nil, err
	}
	manifest.Raw = append([]byte(nil), raw.Body...)
	return &manifest, nil
}
