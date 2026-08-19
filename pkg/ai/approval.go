package ai

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/google/uuid"
)

// ApprovalRequest carries context for a write approval decision.
type ApprovalRequest struct {
	Actor        string
	Subject      string
	Operation    string
	ResourceType string
	ID           string
	Fields       map[string]any
	Preview      any
}

// ApprovalResult is the outcome of an approval request.
type ApprovalResult struct {
	Approved bool
	Token    string
}

// ApprovalStore is the durable approval boundary. Implementations should
// persist pending requests and atomically verify-and-consume approved tokens.
type ApprovalStore interface {
	CreatePending(ctx context.Context, req ApprovalRequest) (string, error)
	VerifyAndConsume(ctx context.Context, token string, req ApprovalRequest) error
}

// MemoryApprovalStore is a small stateful implementation for tests and local
// development. Production deployments should provide a durable implementation.
type MemoryApprovalStore struct {
	mu      sync.Mutex
	entries map[string]memoryApproval
}

type memoryApproval struct {
	key      string
	approved bool
	consumed bool
}

// NewMemoryApprovalStore returns an empty approval store.
func NewMemoryApprovalStore() *MemoryApprovalStore {
	return &MemoryApprovalStore{entries: make(map[string]memoryApproval)}
}

// CreatePending stores a pending approval and returns its opaque token.
func (s *MemoryApprovalStore) CreatePending(_ context.Context, req ApprovalRequest) (string, error) {
	if s == nil {
		return "", ErrMissingApprovalStore
	}
	token := "approval-" + uuid.NewString()
	s.mu.Lock()
	if s.entries == nil {
		s.entries = make(map[string]memoryApproval)
	}
	s.entries[token] = memoryApproval{key: approvalRequestKey(req)}
	s.mu.Unlock()
	return token, nil
}

// Approve marks a pending token approved. It is intended for tests/local use;
// durable systems should expose this transition through their approval service.
func (s *MemoryApprovalStore) Approve(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok {
		return ErrApprovalTokenInvalid
	}
	entry.approved = true
	s.entries[token] = entry
	return nil
}

// VerifyAndConsume validates the token against the exact write request and
// prevents replay.
func (s *MemoryApprovalStore) VerifyAndConsume(_ context.Context, token string, req ApprovalRequest) error {
	if s == nil {
		return ErrMissingApprovalStore
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok || entry.key != approvalRequestKey(req) || !entry.approved || entry.consumed {
		return ErrApprovalTokenInvalid
	}
	entry.consumed = true
	s.entries[token] = entry
	return nil
}

func approvalRequestKey(req ApprovalRequest) string {
	payload := struct {
		Actor        string         `json:"actor"`
		Subject      string         `json:"subject"`
		Operation    string         `json:"operation"`
		ResourceType string         `json:"resourceType"`
		ID           string         `json:"id"`
		Fields       map[string]any `json:"fields"`
	}{
		Actor: req.Actor, Subject: req.Subject, Operation: req.Operation,
		ResourceType: req.ResourceType, ID: req.ID, Fields: req.Fields,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "invalid-approval-request"
	}
	return string(encoded)
}

// ApprovalHook is the optional human approval seam for policy-gated writes.
type ApprovalHook interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error)
}

// ApprovalHookFunc adapts a function to the ApprovalHook interface.
type ApprovalHookFunc func(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error)

// RequestApproval implements ApprovalHook.
func (f ApprovalHookFunc) RequestApproval(ctx context.Context, req ApprovalRequest) (*ApprovalResult, error) {
	return f(ctx, req)
}
