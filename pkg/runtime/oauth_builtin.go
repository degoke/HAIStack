package runtime

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	if state == nil || state.sqliteDB == nil {
		return fmt.Errorf("runtime: builtin oauth requires sqlite storage")
	}
	cfg := b.builtinOAuth
	if err := validateBuiltinOAuthConfig(*cfg); err != nil {
		return err
	}
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

	keySet, err := oauthstore.LoadOrCreateSigningKeySet(state.sqliteDB.SQL(), issuer, oauthstore.SigningKeyOptions{
		ActiveKeyID:      "haistack",
		EncryptionSecret: oauth.SigningKeyEncryptionSecret(),
		RotateOnStartup:  strings.TrimSpace(os.Getenv("OAUTH_SIGNING_KEY_ROTATE")) == "1",
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
	if err := oauthstore.ApplySQLiteStores(&oauthCfg, state.sqliteDB.SQL()); err != nil {
		return fmt.Errorf("runtime: oauth sqlite stores: %w", err)
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

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
