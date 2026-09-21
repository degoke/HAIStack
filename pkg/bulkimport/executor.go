package bulkimport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// ResourceWriter persists imported FHIR resources.
type ResourceWriter interface {
	Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error)
	Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error)
}

// Executor reads NDJSON inputs and upserts resources.
type Executor struct {
	Resources ResourceWriter
	Files     FileStore
	Codec     types.ResourceCodec
}

// ExecuteRequest configures one import execution pass.
type ExecuteRequest struct {
	JobID       string
	Inputs      []InputFile
	IsCancelled func() bool
	OnProgress  func(done, total int)
}

// ExecuteResult summarizes imported counts and error artifacts.
type ExecuteResult struct {
	Output []CountFile
	Errors []ErrorFile
}

// Execute imports NDJSON inputs stored for the job.
func (e *Executor) Execute(ctx context.Context, req ExecuteRequest) (*ExecuteResult, error) {
	if e == nil || e.Resources == nil || e.Files == nil {
		return nil, fmt.Errorf("import: executor is not configured")
	}
	codec := e.Codec
	if codec == nil {
		codec = types.NewJSONCodec()
	}
	result := &ExecuteResult{}
	total := len(req.Inputs)
	for i, input := range req.Inputs {
		if req.IsCancelled != nil && req.IsCancelled() {
			return nil, fmt.Errorf("import cancelled")
		}
		if req.OnProgress != nil {
			req.OnProgress(i, total)
		}
		output, errFile, err := e.importInput(ctx, codec, req.JobID, i, input)
		if err != nil {
			return nil, err
		}
		if output != nil {
			result.Output = append(result.Output, *output)
		}
		if errFile != nil {
			result.Errors = append(result.Errors, *errFile)
		}
	}
	if req.OnProgress != nil {
		req.OnProgress(total, total)
	}
	return result, nil
}

func (e *Executor) importInput(ctx context.Context, codec types.ResourceCodec, jobID string, index int, input InputFile) (*CountFile, *ErrorFile, error) {
	data, _, err := e.Files.Get(ctx, inputPath(jobID, index, input.Type))
	if err != nil {
		return nil, nil, err
	}
	var errBuf bytes.Buffer
	errorsWritten := 0
	imported := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		env, parseErr := codec.ParseJSON(input.Type, line)
		if parseErr != nil {
			writeImportError(&errBuf, &errorsWritten, fmt.Sprintf("parse %s: %v", input.Type, parseErr))
			continue
		}
		if input.Type != "" && env.ResourceType != "" && env.ResourceType != input.Type {
			writeImportError(&errBuf, &errorsWritten, fmt.Sprintf("resourceType %s does not match input type %s", env.ResourceType, input.Type))
			continue
		}
		if _, err := upsertResource(ctx, e.Resources, env); err != nil {
			writeImportError(&errBuf, &errorsWritten, fmt.Sprintf("persist %s/%s: %v", env.ResourceType, env.ID, err))
			continue
		}
		imported++
	}
	output := &CountFile{Type: input.Type, Count: imported}
	if errorsWritten == 0 {
		return output, nil, nil
	}
	errPath := errorPath(jobID, index, input.Type)
	if err := e.Files.Put(ctx, errPath, errBuf.Bytes(), InputFormatNDJSON); err != nil {
		return nil, nil, err
	}
	return output, &ErrorFile{Type: input.Type, URL: errPath}, nil
}

func upsertResource(ctx context.Context, writer ResourceWriter, env *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if env.ID == "" {
		return writer.Create(ctx, env)
	}
	_, err := writer.Read(ctx, env.ResourceType, env.ID)
	if err == nil {
		return writer.Update(ctx, env)
	}
	if !core.IsNotFound(err) {
		return nil, err
	}
	return writer.Create(ctx, env)
}

func writeImportError(buf *bytes.Buffer, count *int, message string) {
	line, _ := json.Marshal(map[string]any{
		"resourceType": "OperationOutcome",
		"issue": []map[string]string{{
			"severity":    "error",
			"code":        "processing",
			"diagnostics": message,
		}},
	})
	if *count > 0 {
		_ = buf.WriteByte('\n')
	}
	_, _ = buf.Write(line)
	*count++
}

func inputPath(jobID string, index int, resourceType string) string {
	if resourceType == "" {
		resourceType = "Resource"
	}
	return fmt.Sprintf("%s/input-%d-%s.ndjson", jobID, index, sanitizeType(resourceType))
}

func errorPath(jobID string, index int, resourceType string) string {
	if resourceType == "" {
		resourceType = "Resource"
	}
	return fmt.Sprintf("%s/error-%d-%s.ndjson", jobID, index, sanitizeType(resourceType))
}

func sanitizeType(resourceType string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, resourceType)
}
