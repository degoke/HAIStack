package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/auth"
	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/smart"
)

// BuiltinOAuthConfig enables the built-in pkg/oauth authorization server during runtime wire.
// IssuerURL defaults to http://{HTTPAddr} when empty.
type BuiltinOAuthConfig struct {
	Production              bool
	RegistrationAccessToken string
	AutoApprove             *bool
	IssuerURL               string
	TenantID                string
}

func (b *Builder) wireBuiltinOAuth(ctx context.Context, state *wireState) error {
	if b == nil || b.builtinOAuth == nil {
		return nil
	}
	if state == nil || state.sqliteDB == nil {
		return fmt.Errorf("runtime: builtin oauth requires sqlite storage")
	}
	cfg := b.builtinOAuth
	issuer := strings.TrimSpace(cfg.IssuerURL)
	if issuer == "" {
		addr := strings.TrimSpace(b.httpAddr)
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		if !strings.Contains(addr, "://") {
			issuer = "http://" + addr
		} else {
			issuer = addr
		}
	}
	issuer = strings.TrimRight(issuer, "/")
	fhirBase := issuer + "/fhir"

	signer, err := oauthstore.LoadOrCreateRS256Signer(state.sqliteDB.SQL(), issuer, "haistack")
	if err != nil {
		return fmt.Errorf("runtime: oauth signing key: %w", err)
	}
	reg := oauth.NewClientRegistry()
	if err := reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "haistack-app",
			RedirectURIs: []string{"http://127.0.0.1/callback", "http://localhost/callback"},
			Scopes:       []string{"openid", "offline_access", "patient/*.read", "user/*.read", "launch/patient"},
		},
		TenantHint: firstNonEmptyString(cfg.TenantID, "local"),
	}); err != nil {
		return fmt.Errorf("runtime: oauth client: %w", err)
	}

	autoApprove := false
	if cfg.AutoApprove != nil {
		autoApprove = *cfg.AutoApprove
	} else if !cfg.Production {
		autoApprove = true
	}

	oauthCfg := oauth.Config{
		Issuer:      issuer,
		FHIRBaseURL: fhirBase,
		Signer:      signer,
		Clients:     reg,
	}
	if err := oauthstore.ApplySQLiteStores(&oauthCfg, state.sqliteDB.SQL()); err != nil {
		return fmt.Errorf("runtime: oauth sqlite stores: %w", err)
	}
	if token := strings.TrimSpace(cfg.RegistrationAccessToken); token != "" {
		oauthCfg.RegistrationAccessToken = token
	} else if v := strings.TrimSpace(os.Getenv("OAUTH_REGISTRATION_TOKEN")); v != "" {
		oauthCfg.RegistrationAccessToken = v
	}
	if cfg.Production {
		if err := oauth.ApplyProductionDefaults(&oauthCfg); err != nil {
			return fmt.Errorf("runtime: oauth production defaults: %w", err)
		}
	}
	consentCtx, stopConsentCleanup := context.WithCancel(context.Background())
	if oauthCfg.ConsentSessionStore != nil {
		oauth.StartConsentSessionCleanup(consentCtx, oauthCfg.ConsentSessionStore, oauth.DefaultConsentSessionCleanupInterval)
	}
	state.cleanup.add(stopConsentCleanup)

	autoApproveVal := autoApprove
	oauthCfg.AutoApprove = &autoApproveVal
	srv, err := oauth.NewServer(oauthCfg)
	if err != nil {
		return fmt.Errorf("runtime: oauth server: %w", err)
	}
	tenantID := firstNonEmptyString(cfg.TenantID, "local")
	wired, err := oauth.WireHTTP(oauth.WireConfig{
		Server: srv,
		Adapter: smart.NewAuthAdapter(smart.AuthAdapterConfig{
			DefaultTenantID:  tenantID,
			DefaultUserRoles: []string{"clinician"},
		}),
	})
	if err != nil {
		return fmt.Errorf("runtime: oauth wire: %w", err)
	}
	engine, err := defaultOAuthPolicyEngine()
	if err != nil {
		return fmt.Errorf("runtime: oauth policy: %w", err)
	}
	checker := wired.ScopePolicyAuthChecker(engine)
	b.oauthHandler = wired.OAuthHandler
	b.httpPrincipalResolver = wired.PrincipalResolver
	b.httpAuthChecker = checker
	return nil
}

func defaultOAuthPolicyEngine() (auth.PolicyEngine, error) {
	return auth.NewEngine(auth.Config{
		Roles: []auth.Role{{
			Name:        "clinician",
			Permissions: []auth.Permission{"Patient.read", "Patient.write", "Patient.search"},
		}},
		PolicyBytes: []byte(`{
  "version": "1",
  "rules": [{
    "name": "allow-clinician-read-write-search",
    "effect": "allow",
    "match": {
      "actions": ["read", "write", "search"],
      "anyPermissions": ["Patient.read", "Patient.write", "Patient.search"]
    },
    "reason": "clinician SMART tokens may access patient resources"
  }]
}`),
		PolicyFormat: auth.PolicyFormatJSON,
	})
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
