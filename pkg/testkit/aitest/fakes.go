package aitest

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/degoke/health-ai-stack/pkg/ai"
	"github.com/degoke/health-ai-stack/pkg/store"
)

var (
	_ ai.AuditLogger  = (*FakeAuditLogger)(nil)
	_ ai.ApprovalHook = (*FakeApprovalHook)(nil)
	_ ai.Deidentifier = (*FakeDeidentifier)(nil)
	_ ai.ModelAdapter = (*FakeModelAdapter)(nil)
)

// FakeAuditLogger records tool access audit events.
type FakeAuditLogger struct {
	mu      sync.Mutex
	Records []ai.AuditRecord
}

func (a *FakeAuditLogger) LogToolAccess(_ context.Context, rec ai.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Records = append(a.Records, rec)
	return nil
}

// FakeApprovalHook returns a configured approval result.
type FakeApprovalHook struct {
	mu       sync.Mutex
	Approved bool
	Token    string
	Store    *ai.MemoryApprovalStore
	Calls    []ai.ApprovalRequest
}

func (h *FakeApprovalHook) RequestApproval(ctx context.Context, req ai.ApprovalRequest) (*ai.ApprovalResult, error) {
	h.mu.Lock()
	h.Calls = append(h.Calls, req)
	approved := h.Approved
	token := h.Token
	store := h.Store
	h.mu.Unlock()
	if approved && store != nil {
		created, err := store.CreatePending(ctx, req)
		if err != nil {
			return nil, err
		}
		if err := store.Approve(created); err != nil {
			return nil, err
		}
		token = created
	}
	return &ai.ApprovalResult{Approved: approved, Token: token}, nil
}

// FakeDeidentifier is a no-op deidentifier that reports one removed field.
type FakeDeidentifier struct {
	Called bool
}

func (d *FakeDeidentifier) Deidentify(_ context.Context, req ai.DeidentifyRequest) (any, []string, error) {
	d.Called = true
	return req.Data, []string{"phone"}, nil
}

// FakeModelAdapter returns deterministic model responses.
type FakeModelAdapter struct {
	AdapterName string
}

func (a *FakeModelAdapter) Name() string { return a.AdapterName }

func (a *FakeModelAdapter) Invoke(_ context.Context, req ai.ModelRequest) (*ai.ModelResponse, error) {
	return &ai.ModelResponse{Adapter: a.AdapterName, Content: "ok-" + req.Hint}, nil
}

// FixedClock returns a pinned time from Now().
type FixedClock struct {
	fixed time.Time
}

// NewFixedClock constructs a fixed clock at t.
func NewFixedClock(t time.Time) *FixedClock { return &FixedClock{fixed: t} }

func (c *FixedClock) Now() time.Time { return c.fixed }

// At returns a UTC time for deterministic AI harness tests.
func At(year int, month time.Month, day, hour, min, sec int) time.Time {
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC)
}

type definitionStore struct {
	records map[string]store.DefinitionResourceRecord
	targets map[string][]store.DefinitionTargetRecord
}

func defKey(url, version string) string { return url + "|" + version }

func newDefinitionStore() *definitionStore {
	return &definitionStore{
		records: make(map[string]store.DefinitionResourceRecord),
		targets: make(map[string][]store.DefinitionTargetRecord),
	}
}

func (s *definitionStore) Upsert(_ context.Context, record store.DefinitionResourceRecord, targets []store.DefinitionTargetRecord) error {
	key := defKey(record.CanonicalURL, record.Version)
	s.records[key] = record
	s.targets[key] = append([]store.DefinitionTargetRecord(nil), targets...)
	return nil
}

func (s *definitionStore) Get(_ context.Context, canonicalURL, version string) (*store.DefinitionResourceRecord, error) {
	record, ok := s.records[defKey(canonicalURL, version)]
	if !ok {
		return nil, errors.New("not found")
	}
	copyRecord := record
	return &copyRecord, nil
}

func (s *definitionStore) Delete(_ context.Context, canonicalURL, version string) error {
	delete(s.records, defKey(canonicalURL, version))
	return nil
}

func (s *definitionStore) List(context.Context, store.DefinitionFilter) ([]store.DefinitionResourceRecord, error) {
	var out []store.DefinitionResourceRecord
	for _, record := range s.records {
		out = append(out, record)
	}
	return out, nil
}

type installStore struct {
	rows []store.RegistryInstallRecord
}

func newInstallStore() *installStore { return &installStore{} }

func (s *installStore) SetEnabled(_ context.Context, record store.RegistryInstallRecord) error {
	s.rows = append(s.rows, record)
	return nil
}

func (s *installStore) UpsertInstall(ctx context.Context, record store.RegistryInstallRecord) error {
	return s.SetEnabled(ctx, record)
}

func (s *installStore) ListEnabled(context.Context) ([]store.RegistryInstallRecord, error) {
	return s.rows, nil
}

func (s *installStore) ListInstalled(context.Context, store.RegistryInstallFilter) ([]store.RegistryInstallRecord, error) {
	return s.rows, nil
}

func (s *installStore) Delete(context.Context, store.RegistryInstallFilter) error { return nil }
