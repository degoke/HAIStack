package binary

import "errors"

var (
	// ErrNotFound is returned when a blob, manifest, link, or session does not exist.
	ErrNotFound = errors.New("binary: not found")

	// ErrAlreadyExists is returned when a create operation conflicts with existing data.
	ErrAlreadyExists = errors.New("binary: already exists")

	// ErrInvalidArgument is returned for malformed inputs or state transitions.
	ErrInvalidArgument = errors.New("binary: invalid argument")

	// ErrUploadIncomplete is returned when finalize is called before all chunks arrive.
	ErrUploadIncomplete = errors.New("binary: upload incomplete")

	// ErrSessionClosed is returned when a transfer session is no longer active.
	ErrSessionClosed = errors.New("binary: session closed")
)
