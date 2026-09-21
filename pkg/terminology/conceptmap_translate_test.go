package terminology

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/audit"
	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestLocalServiceTranslateUsesConceptMap(t *testing.T) {
	raw := []byte(`{
		"resourceType":"ConceptMap",
		"url":"http://example.org/maps/gender",
		"group":[{"element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]
	}`)
	mem := NewMemoryStore()
	if err := mem.PutResource(context.Background(), store.TerminologyResourceRecord{
		ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
		CanonicalURL: "http://example.org/maps/gender", ResourceJSON: raw,
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewLocalService(mem, "default")
	codings, err := svc.Translate(context.Background(), ConceptMapTranslateRequest{
		URL:    "http://example.org/maps/gender",
		Coding: Coding{Code: "F"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(codings) != 1 || codings[0].Code != "female" || codings[0].Equivalence != "equivalent" {
		t.Fatalf("unexpected translation: %#v", codings)
	}
}

func TestTranslateEmitsResolvedMapAudit(t *testing.T) {
	raw := []byte(`{
		"resourceType":"ConceptMap",
		"url":"http://example.org/maps/gender",
		"version":"1.0.0",
		"sourceUri":"http://example.org/source|9",
		"group":[{"source":"http://example.org/source","element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]
	}`)
	mem := NewMemoryStore()
	if err := mem.PutResource(context.Background(), store.TerminologyResourceRecord{
		ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
		CanonicalURL: "http://example.org/maps/gender", Version: "1.0.0", ResourceJSON: raw,
	}); err != nil {
		t.Fatal(err)
	}
	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore}
	svc := NewLocalService(mem, "default", WithTranslateAudit(logger, "tester", "t1", nil))
	if _, err := svc.Translate(context.Background(), ConceptMapTranslateRequest{
		URL: "http://example.org/maps/gender", Version: "1.0.0", Coding: Coding{Code: "F"},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := logger.ListEvents(context.Background(), audit.Query{Action: audit.ActionTerminologyTranslate, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Details["conceptMapUrl"] != "http://example.org/maps/gender" {
		t.Fatalf("url = %v", events[0].Details)
	}
	if events[0].Details["conceptMapVersion"] != "1.0.0" {
		t.Fatalf("version = %v", events[0].Details)
	}
	if events[0].Details["sourceSystemVersion"] != "9" {
		t.Fatalf("sourceSystemVersion = %v", events[0].Details)
	}
}

func TestTranslateAuditUsesResolvedMapNotRequestIdentity(t *testing.T) {
	raw := []byte(`{
		"resourceType":"ConceptMap",
		"url":"http://example.org/maps/gender",
		"group":[{"source":"http://example.org/source","element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]
	}`)
	mem := NewMemoryStore()
	if err := mem.PutResource(context.Background(), store.TerminologyResourceRecord{
		ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
		CanonicalURL: "http://example.org/maps/gender", ResourceJSON: raw,
	}); err != nil {
		t.Fatal(err)
	}
	auditStore := audit.NewMemoryStore()
	logger := &audit.StoreAdapter{Store: auditStore}
	svc := NewLocalService(mem, "default", WithTranslateAudit(logger, "tester", "t1", nil))
	if _, err := svc.Translate(context.Background(), ConceptMapTranslateRequest{
		URL: "http://example.org/maps/gender", Version: "", Coding: Coding{Code: "F"},
	}); err != nil {
		t.Fatal(err)
	}
	events, err := logger.ListEvents(context.Background(), audit.Query{Action: audit.ActionTerminologyTranslate, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(events))
	}
	if events[0].Details["conceptMapVersion"] != "" {
		t.Fatalf("audit must not invent a version the resolved ConceptMap omitted: %v", events[0].Details)
	}
	if events[0].Details["sourceSystemVersion"] != "" {
		t.Fatalf("audit must not invent sourceSystemVersion: %v", events[0].Details)
	}
}
