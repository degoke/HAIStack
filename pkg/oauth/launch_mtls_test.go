package oauth

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"testing"
)

func TestPeerLaunchCertAllowed(t *testing.T) {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "ehr-launcher.example.com"}}
	reqWithCert := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}}
	reqNoCert := &http.Request{}

	srv := &Server{cfg: Config{LaunchIssuerMTLS: &LaunchIssuerMTLSConfig{
		RequireMTLS:        true,
		AllowedCommonNames: []string{"ehr-launcher.example.com"},
	}}}
	if err := srv.peerLaunchCertAllowed(reqNoCert); err == nil {
		t.Fatal("expected require mTLS to reject missing cert")
	}
	if err := srv.peerLaunchCertAllowed(reqWithCert); err != nil {
		t.Fatalf("valid cert: %v", err)
	}

	srv.cfg.LaunchIssuerMTLS.AllowedCommonNames = []string{"other.example.com"}
	if err := srv.peerLaunchCertAllowed(reqWithCert); err == nil {
		t.Fatal("expected CN mismatch to fail")
	}

	srv.cfg.LaunchIssuerMTLS = &LaunchIssuerMTLSConfig{AllowedCommonNames: []string{"ehr-launcher.example.com"}}
	if err := srv.peerLaunchCertAllowed(reqWithCert); err != nil {
		t.Fatalf("optional mTLS with allowlist: %v", err)
	}
}

func TestLaunchAuthenticatedByMTLS(t *testing.T) {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: "ehr"}}
	req := &http.Request{TLS: &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}}
	srv := &Server{cfg: Config{LaunchIssuerMTLS: &LaunchIssuerMTLSConfig{RequireMTLS: true}}}
	if !srv.launchAuthenticatedByMTLS(req) {
		t.Fatal("expected mTLS-only auth to succeed")
	}
	srv.cfg.LaunchIssuerMTLS.RequireMTLS = false
	if srv.launchAuthenticatedByMTLS(req) {
		t.Fatal("expected mTLS-only auth to require RequireMTLS")
	}
}
