package core

import (
	"context"
	"fmt"

	"github.com/degoke/haistack/pkg/hooks"
	"github.com/degoke/haistack/pkg/store"
	"github.com/degoke/haistack/pkg/types"
)

// UpdateIfMatch replaces a resource only when expectedVersion still matches
// the persisted version. The comparison and update occur in one write session.
func (s *ResourceService) UpdateIfMatch(ctx context.Context, resource *types.ResourceEnvelope, expectedVersion string) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, invalidErr("resource envelope is required", nil)
	}
	if expectedVersion == "" {
		return nil, invalidErr("expected version is required", nil)
	}
	envelope, err := s.normalizeEnvelope(resource)
	if err != nil {
		return nil, err
	}
	if envelope.ResourceType == "" || envelope.ID == "" {
		return nil, invalidErr("resourceType and id are required", nil)
	}
	if err := s.idPolicy.Validate(envelope.ResourceType, envelope.ID); err != nil {
		return nil, invalidErr("invalid resource id", err, "Resource.id")
	}
	if s.validator != nil {
		if err := s.validator.ValidateResource(ctx, envelope); err != nil {
			return nil, invalidErr("resource validation failed", err)
		}
	}

	session, err := s.sessions.BeginWrite(ctx)
	if err != nil {
		return nil, exceptionErr("begin write session", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()
	previous, err := session.ResourceStore().Read(ctx, envelope.ResourceType, envelope.ID)
	if err != nil {
		if isStoreNotFound(err) {
			return nil, notFoundErr(fmt.Sprintf("resource not found: %s/%s", envelope.ResourceType, envelope.ID), err)
		}
		return nil, exceptionErr("read previous resource", err)
	}
	written, err := s.applyWriteExpectedVersion(ctx, session, envelope, store.VersionActionUpdate, expectedVersion, hooks.ActionUpdate, previous)
	if err != nil {
		return nil, err
	}
	if err := s.removePreviousTerminology(ctx, session, previous, written); err != nil {
		return nil, exceptionErr("replace previous terminology projection", err)
	}
	if err := session.Commit(ctx); err != nil {
		return nil, exceptionErr("commit write session", err)
	}
	committed = true
	s.runPostCommit(ctx, hooks.ActionUpdate, written, previous)
	return written, nil
}

// DeleteIfMatch removes a resource only when expectedVersion still matches
// the persisted version. The comparison and delete occur in one write session.
func (s *ResourceService) DeleteIfMatch(ctx context.Context, resourceType, id, expectedVersion string) error {
	if resourceType == "" || id == "" || expectedVersion == "" {
		return invalidErr("resourceType, id, and expected version are required", nil)
	}
	if err := s.idPolicy.Validate(resourceType, id); err != nil {
		return invalidErr("invalid resource id", err, "Resource.id")
	}
	session, err := s.sessions.BeginWrite(ctx)
	if err != nil {
		return exceptionErr("begin write session", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = session.Rollback(ctx)
		}
	}()
	current, err := session.ResourceStore().Read(ctx, resourceType, id)
	if err != nil {
		if isStoreNotFound(err) {
			return notFoundErr(fmt.Sprintf("resource not found: %s/%s", resourceType, id), err)
		}
		return exceptionErr("read resource for delete", err)
	}
	original := cloneEnvelope(current)
	deleted, err := s.applyDeleteExpectedVersion(ctx, session, current, expectedVersion)
	if err != nil {
		return err
	}
	if err := session.Commit(ctx); err != nil {
		return exceptionErr("commit write session", err)
	}
	committed = true
	s.runPostCommit(ctx, hooks.ActionDelete, deleted, original)
	return nil
}

// PatchIfMatch applies JSON Patch or FHIR Patch and commits it only when
// expectedVersion still matches the current resource version.
func (s *ResourceService) PatchIfMatch(ctx context.Context, resourceType, id string, patchJSON []byte, expectedVersion string) (*types.ResourceEnvelope, error) {
	if resourceType == "" || id == "" || expectedVersion == "" {
		return nil, invalidErr("resourceType, id, and expected version are required", nil)
	}
	return s.patchAndCommit(ctx, resourceType, id, patchJSON, expectedVersion)
}
