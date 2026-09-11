package runtime

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	oauthstore "github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/jackc/pgx/v5/stdlib"
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
	if err := oauth.RequireSigningKeyEncryptionSecret(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	if cfg.AutoApprove != nil && *cfg.AutoApprove {
		return fmt.Errorf("runtime: production builtin oauth requires AutoApprove false")
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
	fhirBase := issuer + "/fhir"

	dialect := oauthstore.DialectSQLite
	var sqlDB oauthstore.SQLDB
	var applyStores func(*oauth.Config) error
	switch {
	case state.sqliteDB != nil:
		sqlDB = oauthstore.WrapSQLDB(state.sqliteDB.SQL(), oauthstore.DialectSQLite)
		applyStores = func(oauthCfg *oauth.Config) error {
			return oauthstore.ApplySQLiteStores(oauthCfg, state.sqliteDB.SQL())
		}
	case state.postgresDB != nil:
		dialect = oauthstore.DialectPostgres
		sqlDB = oauthstore.WrapSQLDB(stdlib.OpenDBFromPool(state.postgresDB.Pool()), oauthstore.DialectPostgres)
		applyStores = func(oauthCfg *oauth.Config) error {
			return oauthstore.ApplyPostgresStoresDB(oauthCfg, sqlDB)
		}
	default:
		return fmt.Errorf("runtime: builtin oauth requires sqlite or postgres storage")
	}

	keySet, err := oauthstore.LoadOrCreateSigningKeySet(sqlDB, issuer, oauthstore.SigningKeyOptions{
		ActiveKeyID:      "haistack",
		EncryptionSecret: oauth.SigningKeyEncryptionSecret(),
		RotateOnStartup:  strings.TrimSpace(os.Getenv("OAUTH_SIGNING_KEY_ROTATE")) == "1",
		Dialect:          dialect,
	})
	if err != nil {
		return fmt.Errorf("runtime: oauth signing key: %w", err)
	}
	jwksSigners := make([]oauth.TokenSigner, 0, len(keySet.JWKS))
	for _, signer := range keySet.JWKS {
		jwksSigners = append(jwksSigners, signer)
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
		Signer:      keySet.Active,
		JWKSSigners: jwksSigners,
		Clients:     reg,
	}
	if err := applyStores(&oauthCfg); err != nil {
		return fmt.Errorf("runtime: oauth persistence stores: %w", err)
	}
	if token := strings.TrimSpace(cfg.RegistrationAccessToken); token != "" {
		oauthCfg.RegistrationAccessToken = token
	} else if v := strings.TrimSpace(os.Getenv("OAUTH_REGISTRATION_TOKEN")); v != "" {
		oauthCfg.RegistrationAccessToken = v
	}
	autoApproveVal := autoApprove
	oauthCfg.AutoApprove = &autoApproveVal
	oauthCfg.RegisteredClientScopes = oauth.DefaultRegisteredClientScopes()
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

	srv, err := oauth.NewServer(oauthCfg)
	if err != nil {
		return fmt.Errorf("runtime: oauth server: %w", err)
	}
	tenantID := firstNonEmptyString(cfg.TenantID, "local")
	adapter := smart.NewAuthAdapter(smart.AuthAdapterConfig{
		DefaultTenantID: tenantID,
	})
	wired, err := oauth.WireHTTP(oauth.WireConfig{
		Server:  srv,
		Adapter: adapter,
	})
	if err != nil {
		return fmt.Errorf("runtime: oauth wire: %w", err)
	}
	checker := wired.ScopeOnlyAuthChecker(adapter)
	b.oauthHandler = wired.OAuthHandler
	b.httpPrincipalResolver = wired.PrincipalResolver
	b.httpAuthChecker = checker
	return nil
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
