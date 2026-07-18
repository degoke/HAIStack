package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/terminology"
	"github.com/degoke/health-ai-stack/pkg/types"
	"github.com/degoke/health-ai-stack/pkg/validate"
	"github.com/google/uuid"
)

// ResourceService implements haistack-core FHIR resource lifecycle operations.
//
// Use NewResourceService to construct a service. All write paths run inside
// store.WriteSession for atomic persistence of resource, history, index, and events.
type ResourceService struct {
	resources store.ResourceStore
	history   store.HistoryStore
	sessions  store.WriteSessionProvider
	idPolicy  ResourceIDPolicy
	codec     types.ResourceCodec

	validator               validate.Validator
	indexer                 search.Indexer
	outbox                  hasync.Outbox
	terminology             store.TerminologyStore
	terminologyScope        string
	terminologySourceModule string
	terminologyCache        terminology.Invalidator
}

// ResourceServiceConfig configures a ResourceService.
//
// Resources, History, and Sessions are required. IDPolicy and Codec default when nil.
// Validator, Indexer, and Outbox are optional no-ops when nil.
type ResourceServiceConfig struct {
	Resources store.ResourceStore
	History   store.HistoryStore
	Sessions  store.WriteSessionProvider
	IDPolicy  ResourceIDPolicy
	Codec     types.ResourceCodec

	Validator               validate.Validator
	Indexer                 search.Indexer
	Outbox                  hasync.Outbox
	Terminology             store.TerminologyStore
	TerminologyScope        string
	TerminologySourceModule string
	TerminologyCache        terminology.Invalidator
}

// NewResourceService constructs a ResourceService with required dependencies.
func NewResourceService(cfg ResourceServiceConfig) (*ResourceService, error) {
	if cfg.Resources == nil {
		return nil, fmt.Errorf("resources store is required")
	}
	if cfg.History == nil {
		return nil, fmt.Errorf("history store is required")
	}
	if cfg.Sessions == nil {
		return nil, fmt.Errorf("write session provider is required")
	}
	if cfg.IDPolicy == nil {
		cfg.IDPolicy = DefaultIDPolicy{}
	}
	if cfg.Codec == nil {
		cfg.Codec = types.NewJSONCodec()
	}
	if cfg.TerminologyScope == "" {
		cfg.TerminologyScope = "default"
	}
	return &ResourceService{
		resources:               cfg.Resources,
		history:                 cfg.History,
		sessions:                cfg.Sessions,
		idPolicy:                cfg.IDPolicy,
		codec:                   cfg.Codec,
		validator:               cfg.Validator,
		indexer:                 cfg.Indexer,
		outbox:                  cfg.Outbox,
		terminology:             cfg.Terminology,
		terminologyScope:        cfg.TerminologyScope,
		terminologySourceModule: cfg.TerminologySourceModule,
		terminologyCache:        cfg.TerminologyCache,
	}, nil
}

// Create persists a new resource, assigning an ID and version when needed.
func (s *ResourceService) Create(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, invalidErr("resource envelope is required", nil)
	}

	envelope, err := s.normalizeEnvelope(resource)
	if err != nil {
		return nil, err
	}
	if envelope.ResourceType == "" {
		return nil, invalidErr("resourceType is required", nil)
	}

	if s.validator != nil {
		if err := s.validator.ValidateResource(ctx, envelope); err != nil {
			return nil, invalidErr("resource validation failed", err)
		}
	}

	id := envelope.ID
	if id != "" {
		if err := s.idPolicy.Validate(envelope.ResourceType, id); err != nil {
			return nil, invalidErr("invalid resource id", err, "Resource.id")
		}
	} else {
		generated, err := s.idPolicy.Generate(envelope.ResourceType)
		if err != nil {
			return nil, exceptionErr("generate resource id", err)
		}
		id = generated
	}

	envelope.ID = id
	jsonWithID, err := types.SetID(envelope.JSON, id)
	if err != nil {
		return nil, invalidErr("set resource id", err, "Resource.id")
	}
	envelope.JSON = jsonWithID

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

	exists, err := session.ResourceStore().Exists(ctx, envelope.ResourceType, id)
	if err != nil {
		return nil, exceptionErr("check resource existence", err)
	}
	if exists {
		return nil, conflictErr(fmt.Sprintf("resource already exists: %s/%s", envelope.ResourceType, id), nil)
	}

	written, err := s.applyWrite(ctx, session, envelope, store.VersionActionCreate)
	if err != nil {
		return nil, err
	}
	if err := session.Commit(ctx); err != nil {
		return nil, exceptionErr("commit write session", err)
	}
	committed = true
	return written, nil
}

// Read returns the current state of a resource.
func (s *ResourceService) Read(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	if resourceType == "" || id == "" {
		return nil, invalidErr("resourceType and id are required", nil)
	}

	res, err := s.resources.Read(ctx, resourceType, id)
	if err != nil {
		if isStoreNotFound(err) {
			return nil, notFoundErr(fmt.Sprintf("resource not found: %s/%s", resourceType, id), err)
		}
		return nil, exceptionErr("read resource", err)
	}
	return res, nil
}

// Update replaces an existing resource with a new version.
func (s *ResourceService) Update(ctx context.Context, resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if resource == nil {
		return nil, invalidErr("resource envelope is required", nil)
	}

	envelope, err := s.normalizeEnvelope(resource)
	if err != nil {
		return nil, err
	}
	if envelope.ResourceType == "" {
		return nil, invalidErr("resourceType is required", nil)
	}
	if envelope.ID == "" {
		return nil, invalidErr("id is required for update", nil, "Resource.id")
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

	exists, err := session.ResourceStore().Exists(ctx, envelope.ResourceType, envelope.ID)
	if err != nil {
		return nil, exceptionErr("check resource existence", err)
	}
	if !exists {
		return nil, notFoundErr(fmt.Sprintf("resource not found: %s/%s", envelope.ResourceType, envelope.ID), nil)
	}
	previous, err := session.ResourceStore().Read(ctx, envelope.ResourceType, envelope.ID)
	if err != nil {
		return nil, exceptionErr("read previous resource", err)
	}

	written, err := s.applyWrite(ctx, session, envelope, store.VersionActionUpdate)
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
	return written, nil
}

// Delete removes the current resource and records a tombstone history entry.
func (s *ResourceService) Delete(ctx context.Context, resourceType, id string) error {
	if resourceType == "" || id == "" {
		return invalidErr("resourceType and id are required", nil)
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

	if err := s.applyDelete(ctx, session, current); err != nil {
		return err
	}
	if err := session.Commit(ctx); err != nil {
		return exceptionErr("commit write session", err)
	}
	committed = true
	return nil
}

// History returns immutable version history for one resource.
func (s *ResourceService) History(ctx context.Context, resourceType, id string) ([]store.ResourceVersion, error) {
	if resourceType == "" || id == "" {
		return nil, invalidErr("resourceType and id are required", nil)
	}

	versions, err := s.history.GetHistory(ctx, resourceType, id)
	if err != nil {
		return nil, exceptionErr("get resource history", err)
	}
	return versions, nil
}

func (s *ResourceService) normalizeEnvelope(resource *types.ResourceEnvelope) (*types.ResourceEnvelope, error) {
	if len(resource.JSON) == 0 {
		return nil, invalidErr("resource JSON is required", nil)
	}

	normalized, err := types.NormalizeJSON(resource.JSON)
	if err != nil {
		return nil, invalidErr("normalize resource JSON", err)
	}

	envelope, err := s.codec.ParseJSON(resource.ResourceType, normalized)
	if err != nil {
		return nil, invalidErr("parse resource JSON", err)
	}
	if resource.ResourceType != "" && envelope.ResourceType != resource.ResourceType {
		return nil, invalidErr(
			fmt.Sprintf("resourceType mismatch: expected %s, got %s", resource.ResourceType, envelope.ResourceType),
			nil,
		)
	}
	return envelope, nil
}

func (s *ResourceService) applyWrite(
	ctx context.Context,
	session store.WriteSession,
	envelope *types.ResourceEnvelope,
	action store.VersionAction,
) (*types.ResourceEnvelope, error) {
	versionID := uuid.NewString()
	now := time.Now().UTC()

	prepared, err := s.withVersionMeta(envelope, versionID, now)
	if err != nil {
		return nil, err
	}

	switch action {
	case store.VersionActionCreate:
		if err := session.ResourceStore().Create(ctx, prepared); err != nil {
			if isStoreConflict(err) {
				return nil, conflictErr(err.Error(), err)
			}
			return nil, exceptionErr("create resource", err)
		}
	case store.VersionActionUpdate:
		if err := session.ResourceStore().Update(ctx, prepared); err != nil {
			if isStoreNotFound(err) {
				return nil, notFoundErr(err.Error(), err)
			}
			return nil, exceptionErr("update resource", err)
		}
	default:
		return nil, exceptionErr(fmt.Sprintf("unsupported write action %q", action), nil)
	}
	if err := s.compileTerminology(ctx, session, prepared); err != nil {
		return nil, exceptionErr("compile terminology projection", err)
	}

	version := store.ResourceVersion{
		ResourceType: prepared.ResourceType,
		ID:           prepared.ID,
		VersionID:    versionID,
		Action:       action,
		Timestamp:    now,
		Resource:     cloneEnvelope(prepared),
		Hash:         prepared.Hash,
	}
	if err := session.HistoryStore().AppendVersion(ctx, version); err != nil {
		return nil, exceptionErr("append history version", err)
	}

	if err := s.appendEvent(ctx, session, prepared, action, now); err != nil {
		return nil, err
	}

	if err := s.updateSearchIndex(ctx, session, prepared, action); err != nil {
		return nil, err
	}

	return prepared, nil
}

func (s *ResourceService) applyDelete(ctx context.Context, session store.WriteSession, current *types.ResourceEnvelope) error {
	versionID := uuid.NewString()
	now := time.Now().UTC()

	if err := session.ResourceStore().Delete(ctx, current.ResourceType, current.ID); err != nil {
		if isStoreNotFound(err) {
			return notFoundErr(err.Error(), err)
		}
		return exceptionErr("delete resource", err)
	}
	if err := s.removeTerminology(ctx, session, current); err != nil {
		return exceptionErr("delete terminology projection", err)
	}

	version := store.ResourceVersion{
		ResourceType: current.ResourceType,
		ID:           current.ID,
		VersionID:    versionID,
		Action:       store.VersionActionDelete,
		Timestamp:    now,
		Hash:         current.Hash,
		Deleted:      true,
	}
	if err := session.HistoryStore().AppendVersion(ctx, version); err != nil {
		return exceptionErr("append delete history version", err)
	}

	deleteEventEnv := cloneEnvelope(current)
	deleteEventEnv.VersionID = versionID
	deleteEventEnv.LastUpdated = now
	if err := s.appendEvent(ctx, session, deleteEventEnv, store.VersionActionDelete, now); err != nil {
		return err
	}

	if s.indexer != nil {
		if err := session.SearchStore().RemoveIndex(ctx, current.ResourceType, current.ID); err != nil {
			return exceptionErr("remove search index", err)
		}
	}
	return nil
}

func (s *ResourceService) compileTerminology(ctx context.Context, session store.WriteSession, env *types.ResourceEnvelope) error {
	if s.terminology == nil || (env.ResourceType != "CodeSystem" && env.ResourceType != "ValueSet") {
		return nil
	}
	ts, ok := session.(store.TerminologyWriteSession)
	if !ok {
		return nil
	}
	var meta struct{ ResourceType, ID, URL, Version, Status string }
	if err := json.Unmarshal(env.JSON, &meta); err != nil {
		return err
	}
	if meta.URL == "" {
		return fmt.Errorf("terminology url is required")
	}
	r := store.TerminologyResourceRecord{ScopeID: s.terminologyScope, ResourceType: meta.ResourceType, ResourceID: meta.ID, CanonicalURL: meta.URL, Version: meta.Version, Status: meta.Status, ResourceJSON: append([]byte(nil), env.JSON...), SourceModule: s.terminologySourceModule}
	if err := terminology.Install(ctx, ts.TerminologyStore(), r); err != nil {
		return err
	}
	if s.terminologyCache != nil {
		if meta.ResourceType == "CodeSystem" {
			s.terminologyCache.InvalidateCodeSystem(meta.URL, meta.Version)
		} else {
			s.terminologyCache.InvalidateValueSet(meta.URL, meta.Version)
		}
	}
	return nil
}
func (s *ResourceService) removeTerminology(ctx context.Context, session store.WriteSession, env *types.ResourceEnvelope) error {
	if s.terminology == nil || (env.ResourceType != "CodeSystem" && env.ResourceType != "ValueSet") {
		return nil
	}
	ts, ok := session.(store.TerminologyWriteSession)
	if !ok {
		return nil
	}
	var m struct{ URL, Version string }
	if err := json.Unmarshal(env.JSON, &m); err != nil {
		return err
	}
	if m.URL == "" {
		return nil
	}
	if err := ts.TerminologyStore().DeleteProjections(ctx, s.terminologyScope, env.ResourceType, m.URL, m.Version); err != nil {
		return err
	}
	if s.terminologyCache != nil {
		if env.ResourceType == "CodeSystem" {
			s.terminologyCache.InvalidateCodeSystem(m.URL, m.Version)
		} else {
			s.terminologyCache.InvalidateValueSet(m.URL, m.Version)
		}
	}
	return ts.TerminologyStore().DeleteResource(ctx, s.terminologyScope, env.ResourceType, m.URL, m.Version)
}

func (s *ResourceService) removePreviousTerminology(ctx context.Context, session store.WriteSession, previous, current *types.ResourceEnvelope) error {
	if s.terminology == nil || previous == nil || (previous.ResourceType != "CodeSystem" && previous.ResourceType != "ValueSet") {
		return nil
	}
	var oldMeta, newMeta struct{ URL, Version string }
	if err := json.Unmarshal(previous.JSON, &oldMeta); err != nil {
		return err
	}
	if err := json.Unmarshal(current.JSON, &newMeta); err != nil {
		return err
	}
	if oldMeta.URL == newMeta.URL && oldMeta.Version == newMeta.Version {
		return nil
	}
	ts, ok := session.(store.TerminologyWriteSession)
	if !ok {
		return nil
	}
	if err := ts.TerminologyStore().DeleteProjections(ctx, s.terminologyScope, previous.ResourceType, oldMeta.URL, oldMeta.Version); err != nil {
		return err
	}
	if s.terminologyCache != nil {
		if previous.ResourceType == "CodeSystem" {
			s.terminologyCache.InvalidateCodeSystem(oldMeta.URL, oldMeta.Version)
		} else {
			s.terminologyCache.InvalidateValueSet(oldMeta.URL, oldMeta.Version)
		}
	}
	return ts.TerminologyStore().DeleteResource(ctx, s.terminologyScope, previous.ResourceType, oldMeta.URL, oldMeta.Version)
}

func (s *ResourceService) withVersionMeta(envelope *types.ResourceEnvelope, versionID string, now time.Time) (*types.ResourceEnvelope, error) {
	jsonWithMeta, err := types.SetMeta(envelope.JSON, types.Meta{
		VersionID:   versionID,
		LastUpdated: now,
	})
	if err != nil {
		return nil, invalidErr("set resource meta", err, "Resource.meta")
	}
	normalized, err := types.NormalizeJSON(jsonWithMeta)
	if err != nil {
		return nil, invalidErr("normalize resource JSON after meta update", err)
	}
	hash, err := types.HashResource(normalized)
	if err != nil {
		return nil, exceptionErr("hash resource", err)
	}

	out := cloneEnvelope(envelope)
	out.JSON = normalized
	out.VersionID = versionID
	out.LastUpdated = now
	out.Hash = hash
	return out, nil
}

func (s *ResourceService) appendEvent(
	ctx context.Context,
	session store.WriteSession,
	envelope *types.ResourceEnvelope,
	action store.VersionAction,
	timestamp time.Time,
) error {
	if s.outbox == nil {
		return nil
	}

	event := store.ResourceEvent{
		ResourceType: envelope.ResourceType,
		ID:           envelope.ID,
		VersionID:    envelope.VersionID,
		Action:       store.EventAction(action),
		Timestamp:    timestamp,
		Hash:         envelope.Hash,
	}

	outbox := hasync.WithWriteSession(s.outbox, session)
	if _, err := outbox.Append(ctx, event); err != nil {
		return exceptionErr("append outbox event", err)
	}
	return nil
}

func (s *ResourceService) updateSearchIndex(
	ctx context.Context,
	session store.WriteSession,
	envelope *types.ResourceEnvelope,
	action store.VersionAction,
) error {
	if s.indexer == nil {
		return nil
	}
	if action == store.VersionActionDelete {
		return session.SearchStore().RemoveIndex(ctx, envelope.ResourceType, envelope.ID)
	}

	entries, err := s.indexer.Build(ctx, envelope)
	if err != nil {
		return exceptionErr("build search index", err)
	}
	if err := session.SearchStore().RemoveIndex(ctx, envelope.ResourceType, envelope.ID); err != nil {
		return exceptionErr("remove search index", err)
	}
	for _, entry := range entries {
		entry.ResourceType = envelope.ResourceType
		entry.ID = envelope.ID
		if err := session.SearchStore().Index(ctx, entry); err != nil {
			return exceptionErr("index resource", err)
		}
	}
	return nil
}

func cloneEnvelope(src *types.ResourceEnvelope) *types.ResourceEnvelope {
	if src == nil {
		return nil
	}
	out := *src
	if len(src.JSON) > 0 {
		out.JSON = append([]byte(nil), src.JSON...)
	}
	return &out
}

func isStoreNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "resource not found")
}

func isStoreConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "resource already exists")
}
