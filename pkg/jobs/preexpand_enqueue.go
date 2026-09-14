package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/store"
)

// PackPreExpandJobID returns a stable job id for one pack pre-expand attempt.
func PackPreExpandJobID(scopeID, packName, packVersion string) string {
	return fmt.Sprintf("registry.preexpand.%s.%s.%s",
		jobIDPart(scopeID), jobIDPart(packName), jobIDPart(packVersion))
}

func jobIDPart(s string) string {
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, ":", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// EnqueuePackPreExpand enqueues or re-queues a pack pre-expand job.
// Pending and running jobs are left untouched. Completed or failed jobs are reset
// to pending so re-install can trigger another pre-expand pass.
func EnqueuePackPreExpand(ctx context.Context, jobStore store.JobStore, scopeID, packName, packVersion string, tenantID string, now func() time.Time) error {
	if jobStore == nil {
		return ErrNilStore
	}
	if packName == "" || packVersion == "" {
		return fmt.Errorf("packName and packVersion are required")
	}
	payload := TerminologyPreExpandPayload{
		ScopeID:     scopeID,
		PackName:    packName,
		PackVersion: packVersion,
	}
	jobID := PackPreExpandJobID(scopeID, packName, packVersion)
	existing, err := jobStore.Get(ctx, jobID)
	if err == nil && existing != nil {
		switch existing.Status {
		case store.JobStatusPending, store.JobStatusRunning:
			return nil
		case store.JobStatusCompleted, store.JobStatusFailed:
			job, err := NewJob(TypeTerminologyPreExpand, payload, EnqueueOptions{
				ID:          jobID,
				PrincipalID: RegistryPrincipalID,
				TenantID:    tenantID,
				Now:         now,
			})
			if err != nil {
				return err
			}
			job.Status = store.JobStatusPending
			job.Attempts = 0
			job.LastError = ""
			return jobStore.Update(ctx, job)
		}
	}
	_, err = Enqueue(ctx, jobStore, TypeTerminologyPreExpand, payload, EnqueueOptions{
		ID:          jobID,
		PrincipalID: RegistryPrincipalID,
		TenantID:    tenantID,
		Now:         now,
	})
	if errors.Is(err, ErrDuplicateJob) {
		return nil
	}
	return err
}
