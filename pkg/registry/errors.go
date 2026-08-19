package registry

import "errors"

var (
	ErrInvalidDefinition  = errors.New("invalid definition resource")
	ErrMissingDefinition  = errors.New("missing required definition")
	ErrSnapshotCompile    = errors.New("snapshot compilation failed")
	ErrResourceNotEnabled = errors.New("resource type not enabled")
	ErrDefinitionNotFound = errors.New("definition not found")
	ErrDefinitionConflict = errors.New("definition ownership conflict")
)
