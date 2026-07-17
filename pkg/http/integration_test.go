package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/core"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/store"
	"github.com/degoke/health-ai-stack/pkg/types"
)

func openIntegrationStack(t *testing.T) (http.Handler, *core.ResourceService) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.Open(filepath.Join(dir, "http.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	defStore := db.DefinitionStore()
	installStore := db.RegistryInstallStore()
	manager := registry.NewManager(registry.Config{
		Definitions: defStore,
		Installs:    installStore,
		Now:         func() time.Time { return time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC) },
	})
	if err := manager.SeedBundled(ctx); err != nil {
		t.Fatalf("SeedBundled: %v", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		t.Fatalf("EnableResource: %v", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}

	resourceStore := db.ResourceStore()
	searchStore := db.SearchStore()

	svc, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: resourceStore,
		History:   db.HistoryStore(),
		Sessions:  db,
		Indexer:   &familyIndexer{},
	})
	if err != nil {
		t.Fatalf("NewResourceService: %v", err)
	}

	searchSvc, err := search.NewService(search.ServiceConfig{
		Registry:  search.NewSnapshotRegistry(snapshot),
		Executor:  search.NewStoreExecutor(searchStore, resourceStore),
		Resources: resourceStore,
		BaseURL:   "/fhir/Patient",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	handler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:  hahttp.CoreResourceService{Svc: svc},
		SearchService:    hahttp.SearchServiceAdapter{Svc: searchSvc},
		CapabilitySource: hahttp.RegistryCapabilitySource{Snapshot: snapshot},
		ServerMetadata: hahttp.ServerMetadata{
			SoftwareName:    "haistack-http",
			SoftwareVersion: "test",
		},
	})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return handler, svc
}

type familyIndexer struct{}

func (i *familyIndexer) Build(_ context.Context, resource *types.ResourceEnvelope) ([]store.SearchIndexEntry, error) {
	var obj map[string]interface{}
	if err := json.Unmarshal(resource.JSON, &obj); err != nil {
		return nil, err
	}
	family := extractFamily(obj)
	if family == "" {
		return nil, nil
	}
	return []store.SearchIndexEntry{{
		ResourceType: resource.ResourceType,
		ID:           resource.ID,
		Fields:       map[string]string{"string.family": family},
	}}, nil
}

func extractFamily(obj map[string]interface{}) string {
	names, ok := obj["name"].([]interface{})
	if !ok || len(names) == 0 {
		return ""
	}
	first, ok := names[0].(map[string]interface{})
	if !ok {
		return ""
	}
	family, _ := first["family"].(string)
	return family
}

func TestIntegrationCreateReadSearchMetadata(t *testing.T) {
	handler, _ := openIntegrationStack(t)

	createRec := doRequest(t, handler, http.MethodPost, "/fhir/Patient", patientJSON("", "Integration"))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected created id")
	}

	readRec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/"+id, nil)
	if readRec.Code != http.StatusOK {
		t.Fatalf("read status = %d, body = %s", readRec.Code, readRec.Body.String())
	}

	searchRec := doRequest(t, handler, http.MethodGet, "/fhir/Patient?family=Integration", nil)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", searchRec.Code, searchRec.Body.String())
	}
	var searchBundle map[string]interface{}
	if err := json.Unmarshal(searchRec.Body.Bytes(), &searchBundle); err != nil {
		t.Fatalf("unmarshal search: %v", err)
	}
	if searchBundle["type"] != "searchset" {
		t.Fatalf("search type = %v", searchBundle["type"])
	}

	metaRec := doRequest(t, handler, http.MethodGet, "/fhir/metadata", nil)
	if metaRec.Code != http.StatusOK {
		t.Fatalf("metadata status = %d, body = %s", metaRec.Code, metaRec.Body.String())
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(metaRec.Body.Bytes(), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["resourceType"] != "CapabilityStatement" {
		t.Fatalf("metadata type = %v", metadata["resourceType"])
	}
}

func TestIntegrationHistoryAfterUpdate(t *testing.T) {
	handler, svc := openIntegrationStack(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &types.ResourceEnvelope{
		ResourceType: "Patient",
		JSON:         patientJSON("hist-1", "Before"),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = svc.Update(ctx, &types.ResourceEnvelope{
		ResourceType: "Patient",
		ID:           created.ID,
		JSON:         patientJSON(created.ID, "After"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	rec := doRequest(t, handler, http.MethodGet, "/fhir/Patient/"+created.ID+"/_history", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("history status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	entries, ok := bundle["entry"].([]interface{})
	if !ok || len(entries) < 2 {
		t.Fatalf("history entries = %v", bundle["entry"])
	}
}

func TestIntegrationTransactionBundle(t *testing.T) {
	handler, _ := openIntegrationStack(t)
	body := []byte(`{
		"resourceType":"Bundle",
		"type":"transaction",
		"entry":[
			{
				"request":{"method":"POST","url":"Patient"},
				"resource":{"resourceType":"Patient","name":[{"family":"Txn"}]}
			}
		]
	}`)
	rec := doRequest(t, handler, http.MethodPost, "/fhir", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var bundle map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &bundle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bundle["type"] != "transaction-response" {
		t.Fatalf("type = %v", bundle["type"])
	}
}
