package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// IsMissing reports whether err means the job id is absent.
// It accepts ErrJobNotFound and the "job not found:" dialect used by
// pkg/postgres and pkg/sqlite, without treating unrelated errors as missing.
func IsMissing(err error) bool {
	return missingError(err, "job")
}

func missingError(err error, noun string) bool {
	if err == nil {
		return false
	}
	if noun == "job" && errors.Is(err, ErrJobNotFound) {
		return true
	}
	msg := err.Error()
	token := noun + " not found"
	return msg == token || strings.HasPrefix(msg, token+":") || strings.Contains(msg, ": "+token)
}

// GetRecord loads a job row of expectedType. Missing ids and type mismatches
// return (nil, nil). Persistence errors are returned as-is.
func GetRecord(ctx context.Context, db store.JobStore, expectedType, id string) (*store.JobRecord, error) {
	if db == nil {
		return nil, ErrNilStore
	}
	record, err := db.Get(ctx, id)
	if err != nil {
		if IsMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if record == nil || record.Type != expectedType {
		return nil, nil
	}
	return record, nil
}

// Lookup decodes a typed payload from a status record. Missing records return (nil, nil).
func Lookup[T any](ctx context.Context, db store.JobStore, expectedType, id string) (*T, error) {
	record, err := GetRecord(ctx, db, expectedType, id)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, nil
	}
	var value T
	if err := UnmarshalPayload(record.Payload, &value); err != nil {
		return nil, fmt.Errorf("jobs: decode %s %q: %w", expectedType, id, err)
	}
	return &value, nil
}

// WriteRecord updates payload, mapped status, and lastError on an existing row.
func WriteRecord(ctx context.Context, db store.JobStore, record *store.JobRecord, payload any, status store.JobStatus, lastError string) error {
	if db == nil {
		return ErrNilStore
	}
	if record == nil {
		return ErrJobNotFound
	}
	body, err := MarshalPayload(payload)
	if err != nil {
		return fmt.Errorf("jobs: encode %s %q: %w", record.Type, record.ID, err)
	}
	record.Payload = body
	record.Status = status
	record.LastError = lastError
	record.UpdatedAt = time.Now().UTC()
	return db.Update(ctx, *record)
}
