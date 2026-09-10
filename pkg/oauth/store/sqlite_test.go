package store_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"
	"time"

	"github.com/degoke/health-ai-stack/pkg/oauth"
	"github.com/degoke/health-ai-stack/pkg/oauth/store"
	"github.com/degoke/health-ai-stack/pkg/smart"
	"github.com/degoke/health-ai-stack/pkg/sqlite"
)

func TestSQLiteStores_AuthCodeAndRefresh(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.OpenAndMigrate(ctx, filepath.Join(t.TempDir(), "oauth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	stores, err := store.NewSQLiteStores(db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	reg := oauth.NewClientRegistry()
	_ = reg.Register(oauth.Client{
		ClientRegistration: smart.ClientRegistration{
			ClientID:     "app",
			RedirectURIs: []string{"https://app/cb"},
			Scopes:       []string{"patient/Patient.read", "offline_access", "openid"},
		},
	})
	srv, err := oauth.NewServer(oauth.Config{
		Issuer:          "https://example.com",
		FHIRBaseURL:     "https://example.com/fhir",
		Signer:          oauth.RS256Signer{PrivateKey: key},
		Clients:         reg,
		CodeStore:       stores.Codes,
		RefreshStore:    stores.Refresh,
		RevocationStore: stores.Revocation,
	})
	if err != nil {
		t.Fatal(err)
	}

	code, err := stores.Codes.Issue(oauth.AuthCode{
		ClientID:    "app",
		RedirectURI: "https://app/cb",
		Scope:       "patient/Patient.read offline_access openid",
	})
	if err != nil {
		t.Fatal(err)
	}
	exchanged, err := stores.Codes.Exchange(code, "app", "https://app/cb", "")
	if err != nil {
		t.Fatal(err)
	}
	if exchanged.Scope == "" {
		t.Fatal("expected scope on exchanged code")
	}
	_, err = stores.Codes.Exchange(code, "app", "https://app/cb", "")
	if err == nil {
		t.Fatal("expected single-use code failure")
	}

	refresh, err := stores.Refresh.Issue(oauth.RefreshRecord{
		ClientID:  "app",
		Scope:     exchanged.Scope,
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	record, newRefresh, err := stores.Refresh.Rotate(refresh, "app")
	if err != nil {
		t.Fatal(err)
	}
	if record.ClientID != "app" || newRefresh == "" {
		t.Fatalf("rotate = %#v %q", record, newRefresh)
	}
	if err := stores.Revocation.Revoke("jti-1", "access_token", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	revoked, err := stores.Revocation.IsRevoked("jti-1")
	if err != nil || !revoked {
		t.Fatalf("revoked = %v err = %v", revoked, err)
	}
	_ = srv
}
