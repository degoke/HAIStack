package core

import (
	"context"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/types"
)

type mockDefinitionIngestor struct {
	installed     int
	completeCalls int
}

func (m *mockDefinitionIngestor) InstallDefinition(context.Context, []byte, registry.InstallProvenance) error {
	m.installed++
	return nil
}

func (m *mockDefinitionIngestor) CompletePackageInstall(context.Context, registry.InstallProvenance) error {
	m.completeCalls++
	return nil
}

func (m *mockDefinitionIngestor) DeleteDefinition(context.Context, string, string) error {
	return nil
}

func TestSyncDefinitionCatalogCompletesOncePerPackageVersion(t *testing.T) {
	ctx := context.Background()
	ingestor := &mockDefinitionIngestor{}
	svc, err := NewResourceService(ResourceServiceConfig{
		Resources:          stubResourceStore{},
		History:            stubHistoryStore{},
		Sessions:           stubSessionProvider{session: stubWriteSession{}},
		DefinitionIngestor: ingestor,
	})
	if err != nil {
		t.Fatal(err)
	}
	written := []*types.ResourceEnvelope{
		{ResourceType: "ValueSet", JSON: []byte(`{"resourceType":"ValueSet","url":"urn:vs1","version":"1.0"}`)},
		{ResourceType: "ValueSet", JSON: []byte(`{"resourceType":"ValueSet","url":"urn:vs2","version":"1.0"}`)},
		{ResourceType: "ValueSet", JSON: []byte(`{"resourceType":"ValueSet","url":"urn:vs3","version":"2.0"}`)},
	}
	if err := svc.syncDefinitionCatalog(ctx, written, nil); err != nil {
		t.Fatal(err)
	}
	if ingestor.installed != 3 {
		t.Fatalf("installed=%d", ingestor.installed)
	}
	if ingestor.completeCalls != 2 {
		t.Fatalf("completeCalls=%d want 2 unique package versions", ingestor.completeCalls)
	}
}
