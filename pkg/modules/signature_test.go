package modules_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/degoke/health-ai-stack/pkg/modules"
)

func TestEd25519ModuleVerifierCoversManifestAndDefinitions(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	module := modules.Module{
		Path:          t.TempDir(),
		ManifestBytes: []byte(`{"name":"signed","version":"1.0.0"}`),
		Manifest: modules.Manifest{
			Name:            "signed",
			Version:         "1.0.0",
			DefinitionFiles: []string{"definitions/example.json"},
		},
		Definitions: [][]byte{[]byte(`{"resourceType":"SearchParameter","url":"https://example.test/sp","base":["Patient"],"code":"x","type":"string"}`)},
	}
	signature := ed25519.Sign(privateKey, modules.ModuleSigningDigest(module))
	if err := os.WriteFile(filepath.Join(module.Path, "module.json.sig"), []byte(base64.StdEncoding.EncodeToString(signature)), 0o600); err != nil {
		t.Fatalf("write signature: %v", err)
	}

	verifier := modules.Ed25519ModuleVerifier{PublicKey: publicKey}
	if err := verifier.VerifyModule(context.Background(), module); err != nil {
		t.Fatalf("VerifyModule: %v", err)
	}

	module.Definitions[0] = append(module.Definitions[0], ' ')
	if err := verifier.VerifyModule(context.Background(), module); !errors.Is(err, modules.ErrModuleSignatureInvalid) {
		t.Fatalf("tampered VerifyModule = %v, want ErrModuleSignatureInvalid", err)
	}
}

func TestEd25519ModuleVerifierRequiresDetachedSignature(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	verifier := modules.Ed25519ModuleVerifier{PublicKey: publicKey}
	err = verifier.VerifyModule(context.Background(), modules.Module{Path: t.TempDir()})
	if !errors.Is(err, modules.ErrModuleSignatureMissing) {
		t.Fatalf("VerifyModule = %v, want ErrModuleSignatureMissing", err)
	}
}
