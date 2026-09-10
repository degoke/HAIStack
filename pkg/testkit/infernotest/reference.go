package infernotest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/core"
	"github.com/degoke/health-ai-stack/pkg/fhirpath"
	hahttp "github.com/degoke/health-ai-stack/pkg/http"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/registry"
	"github.com/degoke/health-ai-stack/pkg/search"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
	"github.com/degoke/health-ai-stack/pkg/types"
)

const (
	// DefaultClientID is the registered SMART client on the reference host.
	DefaultClientID = "inferno-reference"
	// DefaultRedirectURI is accepted for Inferno launcher redirects.
	DefaultRedirectURI = "https://inferno.healthit.gov/launcher/custom/smart/redirect"
)

// ReferenceMeta describes the URLs exposed by the reference host.
type ReferenceMeta struct {
	BaseURL     string
	FHIRBaseURL string
}

// BuildReferenceHandler wires pkg/oauth and a minimal FHIR API for Inferno discovery checks.
func BuildReferenceHandler(ctx context.Context, baseURL string) (http.Handler, ReferenceMeta, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	baseURL = trimSlash(baseURL)
	if baseURL == "" {
		return nil, ReferenceMeta{}, nil, fmt.Errorf("infernotest: base URL required")
	}
	meta := ReferenceMeta{
		BaseURL:     baseURL,
		FHIRBaseURL: baseURL + "/fhir",
	}

	tempDir, err := os.MkdirTemp("", "haistack-inferno-reference-*")
	if err != nil {
		return nil, ReferenceMeta{}, nil, err
	}
	db, err := sqlite.Open(filepath.Join(tempDir, "oauth.db"))
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, ReferenceMeta{}, nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Migrate(ctx); err != nil {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
		return nil, ReferenceMeta{}, nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	cleanup := func() {
		_ = db.Close()
		_ = os.RemoveAll(tempDir)
	}

	manager := registry.NewManager(registry.Config{
		Definitions: db.DefinitionStore(),
		Installs:    db.RegistryInstallStore(),
	})
	if err := manager.SeedBundled(ctx); err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("seed registry: %w", err)
	}
	if err := manager.EnableResource(ctx, "Patient"); err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("enable Patient: %w", err)
	}
	snapshot, err := manager.RebuildSnapshot(ctx)
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("rebuild snapshot: %w", err)
	}

	engine, err := fhirpath.NewEngine(fhirpath.Config{})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("new fhirpath engine: %w", err)
	}
	indexer, err := search.NewRegistryIndexer(search.RegistryIndexerConfig{
		Registry: search.NewSnapshotRegistry(snapshot),
		Engine:   engine,
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("new search indexer: %w", err)
	}

	resourceService, err := core.NewResourceService(core.ResourceServiceConfig{
		Resources: db.ResourceStore(),
		History:   db.HistoryStore(),
		Sessions:  db,
		Indexer:   indexer,
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("new resource service: %w", err)
	}

	searchService, err := search.NewService(search.ServiceConfig{
		Registry:  search.NewSnapshotRegistry(snapshot),
		Executor:  search.NewStoreExecutor(db.SearchStore(), db.ResourceStore()),
		Resources: db.ResourceStore(),
		BaseURL:   "/fhir",
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("new search service: %w", err)
	}

	patient, err := types.NewJSONCodec().ParseJSON("Patient", []byte(`{
  "resourceType": "Patient",
  "name": [{"given": ["Inferno"], "family": "Patient"}],
  "telecom": [{"system": "phone", "value": "+1-555-0100"}]
}`))
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("parse patient: %w", err)
	}
	created, err := resourceService.Create(ctx, patient)
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("create patient: %w", err)
	}

	authEngine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"Patient.read"},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [{
    "name": "allow-patient-read",
    "effect": "allow",
    "match": {
      "actions": ["read"],
      "resourceTypes": ["Patient"],
      "anyPermissions": ["Patient.read"]
    },
    "reason": "reference host allows patient reads"
  }]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, fmt.Errorf("new auth engine: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}

	reg := oauth.NewClientRegistry()
	if err := reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     DefaultClientID,
			RedirectURIs: []string{DefaultRedirectURI, "http://127.0.0.1/callback"},
			Scopes:       []string{"patient/Patient.read", "launch/patient", "openid", "fhirUser", "offline_access"},
		},
		DefaultPatient: created.ID,
		DefaultUser:    "Practitioner/inferno",
		TenantHint:     "tenant-inferno",
	}); err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}

	oauthCfg := oauth.Config{
		Issuer:      meta.BaseURL,
		FHIRBaseURL: meta.FHIRBaseURL,
		Signer:      oauth.RS256Signer{PrivateKey: key, Kid: "inferno"},
		Clients:     reg,
		ConsentUI: oauth.ConsentUIConfig{
			Title: "HAIStack Inferno Reference",
		},
		LaunchIssuerAuth: &oauth.LaunchIssuerAuth{
			ClientID:     "inferno-ehr",
			ClientSecret: "inferno-ehr-secret",
		},
	}
	if err := oauthstore.ApplySQLiteStores(&oauthCfg, db.SQL()); err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}
	autoApprove := true
	oauthCfg.AutoApprove = &autoApprove
	oauthSrv, err := oauth.NewServer(oauthCfg)
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}
	wired, err := oauth.WireHTTP(oauth.WireConfig{
		Server: oauthSrv,
		Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
			DefaultTenantID:  "tenant-inferno",
			DefaultUserRoles: []string{"clinician"},
		}),
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}

	fhirHandler, err := hahttp.NewHandler(hahttp.Config{
		ResourceService:   hahttp.CoreResourceService{Svc: resourceService},
		SearchService:     hahttp.SearchServiceAdapter{Svc: searchService},
		PrincipalResolver: wired.PrincipalResolver,
		AuthChecker:       wired.ScopePolicyAuthChecker(authEngine),
	})
	if err != nil {
		cleanup()
		return nil, ReferenceMeta{}, nil, err
	}

	handler := oauth.MountRootHandler(fhirHandler, wired.OAuthHandler, nil, nil)
	return handler, meta, cleanup, nil
}

// StartReferenceServer listens on addr and serves the Inferno reference host.
func StartReferenceServer(ctx context.Context, addr string) (*http.Server, ReferenceMeta, func(), error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, ReferenceMeta{}, nil, err
	}
	baseURL := "http://" + listener.Addr().String()
	handler, meta, cleanupStack, err := BuildReferenceHandler(ctx, baseURL)
	if err != nil {
		_ = listener.Close()
		return nil, ReferenceMeta{}, nil, err
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(listener) }()
	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		cleanupStack()
	}
	return srv, meta, cleanup, nil
}

func trimSlash(u string) string {
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}
