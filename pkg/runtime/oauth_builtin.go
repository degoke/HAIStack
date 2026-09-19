package runtime

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
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
	StateDir                string
}

func validateBuiltinOAuthConfig(cfg BuiltinOAuthConfig) error {
	if !cfg.Production {
		return nil
	}
	issuer := strings.TrimSpace(cfg.IssuerURL)
	if issuer == "" {
		return fmt.Errorf("runtime: production builtin oauth requires issuer URL")
	}
	if err := oauth.ValidateProductionIssuer(issuer); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	token := strings.TrimSpace(cfg.RegistrationAccessToken)
	if token == "" {
		token = strings.TrimSpace(os.Getenv("OAUTH_REGISTRATION_TOKEN"))
	}
	if token == "" {
		return fmt.Errorf("runtime: production builtin oauth requires OAUTH_REGISTRATION_TOKEN")
	}
	if cfg.AutoApprove != nil && *cfg.AutoApprove {
		return fmt.Errorf("runtime: production builtin oauth requires AutoApprove false")
	}
	if strings.TrimSpace(os.Getenv("OAUTH_SIGNING_KEY_ENCRYPTION_SECRET")) == "" {
		return fmt.Errorf("runtime: production builtin oauth requires OAUTH_SIGNING_KEY_ENCRYPTION_SECRET")
	}
	if strings.TrimSpace(os.Getenv("OAUTH_SESSION_SECRET")) == "" {
		return fmt.Errorf("runtime: production builtin oauth requires OAUTH_SESSION_SECRET")
	}
	return nil
}

func (b *Builder) wireBuiltinOAuth(ctx context.Context, state *wireState) error {
	if b == nil || b.builtinOAuth == nil {
		return nil
	}
	if state == nil {
		return fmt.Errorf("runtime: builtin oauth requires persistence")
	}
	cfg := b.builtinOAuth
	if err := validateBuiltinOAuthConfig(*cfg); err != nil {
		return err
	}
	issuer, err := resolveBuiltinOAuthIssuer(cfg.IssuerURL, b.httpAddr)
	if err != nil {
		return err
	}

	oauthCfg := oauth.Config{
		Issuer:       issuer,
		FHIRAudience: issuer,
	}
	switch {
	case state.sqliteDB != nil:
		if err := oauthstore.ApplySQLiteStores(&oauthCfg, state.sqliteDB.SQL()); err != nil {
			return fmt.Errorf("runtime: oauth sqlite stores: %w", err)
		}
		if err := b.applyBuiltinSigningKey(&oauthCfg, state, issuer); err != nil {
			return fmt.Errorf("runtime: oauth signing key: %w", err)
		}
	case state.postgresDB != nil:
		if err := oauthstore.ApplyPostgresStores(&oauthCfg, state.postgresDB.Pool()); err != nil {
			return fmt.Errorf("runtime: oauth postgres stores: %w", err)
		}
		if err := b.applyBuiltinSigningKey(&oauthCfg, state, issuer); err != nil {
			return fmt.Errorf("runtime: oauth signing key: %w", err)
		}
	default:
		return fmt.Errorf("runtime: builtin oauth requires sqlite or postgres storage")
	}

	autoApprove := false
	if cfg.AutoApprove != nil {
		autoApprove = *cfg.AutoApprove
	} else if !cfg.Production {
		autoApprove = true
	}
	oauthCfg.AutoApprove = autoApprove

	regToken := strings.TrimSpace(cfg.RegistrationAccessToken)
	if regToken == "" {
		regToken = strings.TrimSpace(os.Getenv("OAUTH_REGISTRATION_TOKEN"))
	}
	if cfg.Production {
		oauthCfg.RequireConsentForm = true
		oauthCfg.AutoApprove = false
		oauthCfg.AllowDynamicRegistration = true
		oauthCfg.LoginPath = "/oauth/login"
		if regToken == "" {
			return fmt.Errorf("runtime: production builtin oauth requires OAUTH_REGISTRATION_TOKEN")
		}
		oauthCfg.RegistrationAccessToken = regToken
		sessionAuth, err := oauth.NewSessionUserAuthenticator(oauth.SessionAuthConfig{})
		if err != nil {
			return fmt.Errorf("runtime: oauth session auth: %w", err)
		}
		oauthCfg.UserAuthenticator = sessionAuth
		if err := oauth.ApplyProductionDefaults(&oauthCfg); err != nil {
			return fmt.Errorf("runtime: oauth production defaults: %w", err)
		}
	} else if regToken != "" {
		oauthCfg.AllowDynamicRegistration = true
		oauthCfg.RegistrationAccessToken = regToken
	}

	tenantID := firstNonEmptyString(cfg.TenantID, "local")
	if err := oauthCfg.Clients.Register(oauth.Client{
		ClientID:     "haistack-app",
		RedirectURIs: []string{"http://127.0.0.1/callback", "http://localhost/callback"},
		Scopes:       []string{"openid", "offline_access", "patient/*.read", "user/*.read", "launch/patient"},
	}); err != nil {
		return fmt.Errorf("runtime: oauth client: %w", err)
	}

	srv, err := oauth.NewServer(oauthCfg)
	if err != nil {
		return fmt.Errorf("runtime: oauth server: %w", err)
	}

	tenantRegistry := oauth.NewTenantRegistry()
	tenantIssuer := strings.TrimRight(issuer, "/") + "/t/" + tenantID
	autoApprovePtr := oauthCfg.AutoApprove
	if err := tenantRegistry.Register(oauth.TenantIssuerConfig{
		TenantID:          tenantID,
		Issuer:            tenantIssuer,
		FHIRAudience:      issuer,
		SigningKey:        oauthCfg.SigningKey,
		Clients:           oauthCfg.Clients,
		UserAuthenticator: oauthCfg.UserAuthenticator,
		LoginPath:         oauthCfg.LoginPath,
		AutoApprove:       &autoApprovePtr,
	}); err != nil {
		return fmt.Errorf("runtime: oauth tenant registry: %w", err)
	}
	multiTenant, err := oauth.NewMultiTenantServer(oauth.MultiTenantConfig{
		Base:    oauthCfg,
		Tenants: tenantRegistry,
	})
	if err != nil {
		return fmt.Errorf("runtime: oauth multi-tenant server: %w", err)
	}

	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID:  tenantID,
		DefaultUserRoles: []string{"clinician"},
	})
	mtBearer := oauth.MultiTenantBearerAuth{
		Base:    srv,
		Tenants: multiTenant,
		Adapter: adapter,
	}
	engine, err := auth.NewEngine(auth.Config{
		Roles: []auth.Role{{Name: "clinician", Permissions: []auth.Permission{"*.read", "*.write", "Patient.read"}}},
		PolicyBytes: []byte(`{
			"version":"1",
			"rules":[
				{"name":"read","effect":"allow","match":{"actions":["read","search"],"anyPermissions":["*.read"]}},
				{"name":"write","effect":"allow","match":{"actions":["write"],"anyPermissions":["*.write"]}}
			]
		}`),
	})
	if err != nil {
		return fmt.Errorf("runtime: oauth auth engine: %w", err)
	}

	b.oauthHandler = oauth.CombineHandlers(srv.Handler(), multiTenant.Handler())
	b.oauthAuthStore = oauthCfg.AuthorizationStore
	b.oauthIssuerURL = issuer
	b.httpPrincipalResolver = mtBearer.PrincipalResolver()
	b.httpAuthBundleResolver = mtBearer.BundleResolver()
	b.httpAuthChecker = smart.ScopePolicyAuthChecker{Engine: engine, Adapter: adapter}
	return nil
}

func (b *Builder) applyBuiltinSigningKey(cfg *oauth.Config, state *wireState, issuer string) error {
	opts := oauthstore.SigningKeyOptions{
		ActiveKeyID:      "haistack",
		EncryptionSecret: oauth.SigningKeyEncryptionSecret(),
		RotateOnStartup:  oauth.SigningKeyRotateOnStartup(),
	}
	if opts.EncryptionSecret != "" {
		switch {
		case state.sqliteDB != nil:
			return oauthstore.ApplySQLiteSigningKey(cfg, state.sqliteDB.SQL(), issuer, opts)
		case state.postgresDB != nil:
			return oauthstore.ApplyPostgresSigningKey(cfg, state.postgresDB.Pool(), issuer, opts)
		}
	}
	stateDir := strings.TrimSpace(b.builtinOAuth.StateDir)
	if stateDir == "" {
		stateDir = b.defaultOAuthStateDir(state)
	}
	signingKeyPath := oauth.DefaultSigningKeyPaths(stateDir).SigningKey
	keySet, err := oauth.LoadOrCreateSigningKey(signingKeyPath, "haistack")
	if err != nil {
		return err
	}
	cfg.SigningKey = keySet
	return nil
}

func (b *Builder) defaultOAuthStateDir(state *wireState) string {
	if strings.TrimSpace(b.dataDir) != "" {
		return filepath.Join(b.dataDir, "oauth")
	}
	if state.sqliteDB != nil && b.sqlitePath != "" {
		return filepath.Join(filepath.Dir(b.sqlitePath), "oauth")
	}
	return ".haistack/oauth"
}

func resolveBuiltinOAuthIssuer(issuerURL, httpAddr string) (string, error) {
	issuer := strings.TrimSpace(issuerURL)
	if issuer != "" {
		return strings.TrimRight(issuer, "/"), nil
	}
	addr := strings.TrimSpace(httpAddr)
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if strings.Contains(addr, "://") {
		return strings.TrimRight(addr, "/"), nil
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("runtime: builtin oauth issuer from http addr %q: %w", addr, err)
	}
	if port == "0" {
		return "", fmt.Errorf("runtime: builtin oauth requires explicit IssuerURL when HTTP listen address uses port 0")
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return strings.TrimRight("http://"+net.JoinHostPort(host, port), "/"), nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
