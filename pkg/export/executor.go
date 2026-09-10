package export

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceLister enumerates FHIR resources for export.
type ResourceLister interface {
	ListIDs(ctx context.Context, resourceType string, limit, offset int) ([]string, error)
	Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
}

// TypeCatalog returns exportable resource types when _type is omitted.
type TypeCatalog interface {
	EnabledResourceTypes() []string
}

// Executor scans resources and writes NDJSON export artifacts.
type Executor struct {
	Resources ResourceLister
	Types     TypeCatalog
	Files     FileStore
	Now       func() time.Time
}

// ExecuteRequest configures one export execution pass.
type ExecuteRequest struct {
	JobID         string
	ResourceTypes []string
	Since         time.Time
	GroupID       string
	PatientIDs    []string
	BaseFileURL   string
	OnProgress    func(done, total int)
	IsCancelled   func() bool
}

// ExecuteResult summarizes generated artifacts.
type ExecuteResult struct {
	Output []OutputFile
	Errors []ErrorFile
}

// Execute exports resources into NDJSON files stored in FileStore.
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if e == nil || e.Resources == nil || e.Files == nil {
		return nil, fmt.Errorf("export: executor is not configured")
	}
	types := req.ResourceTypes
	if len(types) == 0 {
		if e.Types != nil {
			types = e.Types.EnabledResourceTypes()
		}
	}
	if len(types) == 0 {
		types = []string{"Patient", "Observation", "Appointment"}
	}

	result := &ExecuteResult{}
	totalTypes := len(types)
	for i, resourceType := range types {
		if req.IsCancelled != nil && req.IsCancelled() {
			return nil, fmt.Errorf("export cancelled")
		}
		if req.OnProgress != nil {
			req.OnProgress(i, totalTypes)
		}
		output, errFiles, err := e.exportType(ctx, req, resourceType)
		if err != nil {
			return nil, err
		}
		if output != nil {
			result.Output = append(result.Output, *output)
		}
		result.Errors = append(result.Errors, errFiles...)
	}
	if req.OnProgress != nil {
		req.OnProgress(totalTypes, totalTypes)
	}
	return result, nil
}

func (e *Executor) exportType(ctx context.Context, req ExecuteRequest, resourceType string) (*OutputFile, []ErrorFile, error) {
	var buf bytes.Buffer
	var errBuf bytes.Buffer
	exported := 0
	errorsWritten := 0

	ids, err := e.listAllIDs(ctx, resourceType)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		if req.IsCancelled != nil && req.IsCancelled() {
			return nil, nil, fmt.Errorf("export cancelled")
		}
		env, err := e.Resources.Read(ctx, resourceType, id)
		if err != nil {
			return nil, nil, fmt.Errorf("read %s/%s: %w", resourceType, id, err)
		}
		if !req.Since.IsZero() && !env.LastUpdated.IsZero() && env.LastUpdated.Before(req.Since) {
			continue
		}
		if len(req.PatientIDs) > 0 && !resourceMatchesPatients(env, req.PatientIDs) {
			continue
		}
		line, err := json.Marshal(json.RawMessage(env.JSON))
		if err != nil {
			errLine, _ := json.Marshal(map[string]string{
				"resourceType": "OperationOutcome",
				"issue":        fmt.Sprintf("marshal %s/%s: %v", resourceType, id, err),
			})
			if errorsWritten > 0 {
				_ = errBuf.WriteByte('\n')
			}
			_, _ = errBuf.Write(errLine)
			errorsWritten++
			continue
		}
		if exported > 0 {
			_ = buf.WriteByte('\n')
		}
		_, _ = buf.Write(line)
		exported++
	}

	var output *OutputFile
	if exported > 0 {
		path := filePath(req.JobID, resourceType)
		if err := e.Files.Put(ctx, path, buf.Bytes(), "application/fhir+ndjson"); err != nil {
			return nil, nil, err
		}
		output = &OutputFile{
			Type: resourceType,
			URL:  req.BaseFileURL + "/" + resourceType + ".ndjson",
		}
	}

	var errFiles []ErrorFile
	if errorsWritten > 0 {
		errPath := errorFilePath(req.JobID, resourceType)
		if err := e.Files.Put(ctx, errPath, errBuf.Bytes(), "application/fhir+ndjson"); err != nil {
			return nil, nil, err
		}
		errFiles = append(errFiles, ErrorFile{
			Type: resourceType,
			URL:  req.BaseFileURL + "/" + resourceType + ".error.ndjson",
		})
	}
	return output, errFiles, nil
}

func (e *Executor) listAllIDs(ctx context.Context, resourceType string) ([]string, error) {
	var all []string
	pageSize := 100
	for {
		ids, err := e.Resources.ListIDs(ctx, resourceType, pageSize, len(all))
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			break
		}
		all = append(all, ids...)
	}
	return all, nil
}

func filePath(jobID, resourceType string) string {
	return jobID + "/" + resourceType + ".ndjson"
}

func errorFilePath(jobID, resourceType string) string {
	return jobID + "/" + resourceType + ".error.ndjson"
}

func resourceMatchesPatients(env *types.ResourceEnvelope, patientIDs []string) bool {
	if env == nil {
		return false
	}
	if env.ResourceType == "Patient" {
		for _, pid := range patientIDs {
			if env.ID == pid {
				return true
			}
		}
		return false
	}
	refFields := []string{"subject", "patient", "beneficiary", "individual"}
	for _, field := range refFields {
		ref, ok := env.StringField(field, "reference")
		if !ok {
			continue
		}
		for _, pid := range patientIDs {
			if referenceMatchesPatient(ref, pid) {
				return true
			}
		}
	}
	return false
}

func referenceMatchesPatient(reference, patientID string) bool {
	reference = strings.TrimSpace(reference)
	if reference == "" || patientID == "" {
		return false
	}
	want := "Patient/" + patientID
	if reference == want {
		return true
	}
	if strings.HasSuffix(reference, "/"+want) {
		return true
	}
	return false
}

// ParseGroupPatientIDs extracts Patient member ids from a Group resource envelope.
func ParseGroupPatientIDs(env *types.ResourceEnvelope) ([]string, error) {
	if env == nil || env.ResourceType != "Group" {
		return nil, fmt.Errorf("export: group resource required")
	}
	var group map[string]any
	if err := json.Unmarshal(env.JSON, &group); err != nil {
		return nil, err
	}
	members, _ := group["member"].([]any)
	var ids []string
	for _, item := range members {
		member, ok := item.(map[string]any)
		if !ok {
			continue
		}
		entity, ok := member["entity"].(map[string]any)
		if !ok {
			continue
		}
		ref, _ := entity["reference"].(string)
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		parts := strings.Split(ref, "/")
		if len(parts) == 2 && parts[0] == "Patient" {
			ids = append(ids, parts[1])
		}
	}
	return ids, nil
}
