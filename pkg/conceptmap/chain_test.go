package conceptmap

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/store"
)

func TestChainResolverUsesFallbackResolver(t *testing.T) {
	primary := StaticResolver{}
	secondary := StaticResolver{
		"http://example.org/maps/gender": Map{
			URL: "http://example.org/maps/gender",
			Group: []Group{{
				Element: []Element{{
					Code: "M",
					Target: []Target{{
						Code:        "male",
						Equivalence: "equivalent",
					}},
				}},
			}},
		},
	}
	resolver := ChainResolver{Resolvers: []Resolver{primary, secondary}}
	m, err := resolver.Resolve(context.Background(), "http://example.org/maps/gender")
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != "http://example.org/maps/gender" {
		t.Fatalf("unexpected map: %#v", m)
	}
}

func TestTerminologyStoreResolverResolvesConceptMap(t *testing.T) {
	raw := []byte(`{
		"resourceType":"ConceptMap",
		"url":"http://example.org/maps/gender",
		"group":[{"element":[{"code":"F","target":[{"code":"female","equivalence":"equivalent"}]}]}]
	}`)
	mem := &memTerminologyStore{records: map[string]store.TerminologyResourceRecord{
		"default|ConceptMap|http://example.org/maps/gender|": store.TerminologyResourceRecord{
			ScopeID: "default", ResourceType: "ConceptMap", ResourceID: "map-1",
			CanonicalURL: "http://example.org/maps/gender", ResourceJSON: raw,
		},
	}}
	resolver := &TerminologyStoreResolver{Store: mem, ScopeID: "default"}
	m, err := resolver.Resolve(context.Background(), "http://example.org/maps/gender")
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != "http://example.org/maps/gender" {
		t.Fatalf("unexpected map: %#v", m)
	}
}

type memTerminologyStore struct {
	records map[string]store.TerminologyResourceRecord
}

func (m *memTerminologyStore) FindResource(_ context.Context, scopeID, resourceType, canonicalURL, version string) (*store.TerminologyResourceRecord, error) {
	key := scopeID + "|" + resourceType + "|" + canonicalURL + "|" + version
	if record, ok := m.records[key]; ok {
		return &record, nil
	}
	return nil, nil
}

func (m *memTerminologyStore) PutResource(context.Context, store.TerminologyResourceRecord) error { return nil }
func (m *memTerminologyStore) DeleteResource(context.Context, string, string, string, string) error { return nil }
func (m *memTerminologyStore) ListResources(context.Context, string, string) ([]store.TerminologyResourceRecord, error) {
	return nil, nil
}
func (m *memTerminologyStore) ReplaceCodeSystem(context.Context, string, string, string, []store.TerminologyConceptRecord) error {
	return nil
}
func (m *memTerminologyStore) LookupConcept(context.Context, string, string, string, string) (*store.TerminologyConceptRecord, error) {
	return nil, nil
}
func (m *memTerminologyStore) ReplaceValueSet(context.Context, store.TerminologyValueSetRecord, []store.TerminologyExpansionMemberRecord) error {
	return nil
}
func (m *memTerminologyStore) GetValueSet(context.Context, string, string, string) (*store.TerminologyValueSetRecord, error) {
	return nil, nil
}
func (m *memTerminologyStore) ListValueSetMembers(context.Context, string, string, string) ([]store.TerminologyExpansionMemberRecord, error) {
	return nil, nil
}
func (m *memTerminologyStore) DeleteProjections(context.Context, string, string, string, string) error {
	return nil
}
