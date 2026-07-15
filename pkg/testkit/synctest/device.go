package synctest

import (
	"context"
	"fmt"
	"time"

	"github.com/degoke/health-ai-stack/pkg/conflict"
	"github.com/degoke/health-ai-stack/pkg/store"
	hasync "github.com/degoke/health-ai-stack/pkg/sync"
	"github.com/degoke/health-ai-stack/pkg/testkit/storetest"
	"github.com/degoke/health-ai-stack/pkg/types"
)

// Device is a fake sync device node with in-memory stores and an engine.
type Device struct {
	NodeID   string
	TenantID string
	Backend  *storetest.Backend
	Engine   *hasync.Engine
	Clock    hasync.Clock
}

// NewDevice constructs a device node wired to the given hub.
func NewDevice(nodeID, tenantID string, hub hasync.Hub, clock hasync.Clock) *Device {
	backend := storetest.NewDeviceBackend()
	if clock == nil {
		clock = hasync.DefaultClock
	}
	engine := hasync.NewEngine(hasync.Config{
		NodeID:    nodeID,
		TenantID:  tenantID,
		Events:    backend.Events,
		Cursors:   backend.Cursors,
		Inbox:     backend.Inbox,
		Resources: backend.Resources,
		History:   backend.History,
		Conflicts: backend.Conflicts,
		Jobs:      backend.Jobs,
		Audit:     backend.Audit,
		Search:    backend.Search,
		Hub:       hub,
		Clock:     clock,
	})
	return &Device{
		NodeID:   nodeID,
		TenantID: tenantID,
		Backend:  backend,
		Engine:   engine,
		Clock:    clock,
	}
}

// WithConflictEngine attaches a conflict engine to the device sync config.
func (d *Device) WithConflictEngine(engine *conflict.Engine) *Device {
	cfg := d.Engine.Config
	cfg.ConflictEngine = engine
	d.Engine = hasync.NewEngine(cfg)
	return d
}

// SeedLocalCreate records an offline resource create on the device outbox.
func (d *Device) SeedLocalCreate(ctx context.Context, res *types.ResourceEnvelope, ts time.Time) error {
	if res == nil {
		return fmt.Errorf("synctest.SeedLocalCreate: resource is nil")
	}
	if ts.IsZero() {
		ts = d.Clock()
	}
	if err := d.Backend.Resources.Create(ctx, res); err != nil {
		return err
	}
	if err := d.Backend.History.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.VersionActionCreate,
		Timestamp:    ts,
		Resource:     res,
		Hash:         res.Hash,
	}); err != nil {
		return err
	}
	_, err := d.Backend.Events.Append(ctx, store.ResourceEvent{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.EventActionCreate,
		Timestamp:    ts,
		Hash:         res.Hash,
	})
	return err
}

// SeedLocalUpdate records an offline resource update on the device outbox.
func (d *Device) SeedLocalUpdate(ctx context.Context, res *types.ResourceEnvelope, ts time.Time) error {
	if res == nil {
		return fmt.Errorf("synctest.SeedLocalUpdate: resource is nil")
	}
	if ts.IsZero() {
		ts = d.Clock()
	}
	if err := d.Backend.Resources.Update(ctx, res); err != nil {
		return err
	}
	if err := d.Backend.History.AppendVersion(ctx, store.ResourceVersion{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.VersionActionUpdate,
		Timestamp:    ts,
		Resource:     res,
		Hash:         res.Hash,
	}); err != nil {
		return err
	}
	_, err := d.Backend.Events.Append(ctx, store.ResourceEvent{
		ResourceType: res.ResourceType,
		ID:           res.ID,
		VersionID:    res.VersionID,
		Action:       store.EventActionUpdate,
		Timestamp:    ts,
		Hash:         res.Hash,
	})
	return err
}

// Push runs one push pass on the device.
func (d *Device) Push(ctx context.Context) (*hasync.PushResultSummary, error) {
	return d.Engine.Push(ctx)
}

// Pull runs one pull pass on the device.
func (d *Device) Pull(ctx context.Context) (*hasync.PullResultSummary, error) {
	return d.Engine.Pull(ctx)
}

// ResourceExists reports whether a resource is stored locally.
func (d *Device) ResourceExists(ctx context.Context, resourceType, id string) (bool, error) {
	return d.Backend.Resources.Exists(ctx, resourceType, id)
}

// ReadResource reads a resource from the local store.
func (d *Device) ReadResource(ctx context.Context, resourceType, id string) (*types.ResourceEnvelope, error) {
	return d.Backend.Resources.Read(ctx, resourceType, id)
}
