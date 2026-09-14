package terminology

import (
	"context"
	"testing"

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
	if len(codings) != 1 || codings[0].Code != "female" {
		t.Fatalf("unexpected translation: %#v", codings)
	}
}
