package modules

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	defaultModuleSignaturePath = "module.json.sig"
	maxModuleSignatureBytes    = 1024
)

// ModuleVerifier verifies a loaded module before dependency resolution or
// registry mutation. Deployments can use this seam to enforce their own trust
// roots and signature formats.
type ModuleVerifier interface {
	VerifyModule(ctx context.Context, module Module) error
}

// ModuleVerifierFunc adapts a function to ModuleVerifier.
type ModuleVerifierFunc func(ctx context.Context, module Module) error

// VerifyModule implements ModuleVerifier.
func (f ModuleVerifierFunc) VerifyModule(ctx context.Context, module Module) error {
	return f(ctx, module)
}

// Ed25519ModuleVerifier verifies a detached Ed25519 signature stored next to
// module.json. The signature covers ModuleSigningDigest, which includes the
// exact manifest bytes and every loaded definition file in manifest order.
// Signatures may be raw 64-byte values or standard base64 text.
type Ed25519ModuleVerifier struct {
	PublicKey     ed25519.PublicKey
	SignaturePath string
}

// VerifyModule verifies one module using the configured Ed25519 public key.
func (v Ed25519ModuleVerifier) VerifyModule(ctx context.Context, module Module) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(v.PublicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: Ed25519 public key must be %d bytes", ErrModuleSignatureInvalid, ed25519.PublicKeySize)
	}
	if module.Path == "" {
		return fmt.Errorf("%w: module path is empty", ErrModuleSignatureInvalid)
	}
	name := v.SignaturePath
	if name == "" {
		name = defaultModuleSignaturePath
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("%w: signature path must be relative to module directory", ErrModuleSignatureInvalid)
	}
	signaturePath := filepath.Join(module.Path, name)
	if !isPathUnderRoot(module.Path, signaturePath) {
		return fmt.Errorf("%w: signature path escapes module directory", ErrModuleSignatureInvalid)
	}
	realRoot, err := filepath.EvalSymlinks(module.Path)
	if err != nil {
		return fmt.Errorf("resolve module directory for signature: %w", err)
	}
	realSignaturePath, err := filepath.EvalSymlinks(signaturePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrModuleSignatureMissing, name)
		}
		return fmt.Errorf("resolve module signature: %w", err)
	}
	if !isPathUnderRoot(realRoot, realSignaturePath) {
		return fmt.Errorf("%w: signature symlink escapes module directory", ErrModuleSignatureInvalid)
	}
	file, err := os.Open(realSignaturePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrModuleSignatureMissing, name)
		}
		return fmt.Errorf("read module signature: %w", err)
	}
	defer func() { _ = file.Close() }()
	signature, err := io.ReadAll(io.LimitReader(file, maxModuleSignatureBytes+1))
	if err != nil {
		return fmt.Errorf("read module signature: %w", err)
	}
	if len(signature) > maxModuleSignatureBytes {
		return fmt.Errorf("%w: signature exceeds %d bytes", ErrModuleSignatureInvalid, maxModuleSignatureBytes)
	}
	signature = bytes.TrimSpace(signature)
	if len(signature) != ed25519.SignatureSize {
		decoded, decodeErr := base64.StdEncoding.DecodeString(string(signature))
		if decodeErr != nil {
			return fmt.Errorf("%w: decode signature: %v", ErrModuleSignatureInvalid, decodeErr)
		}
		signature = decoded
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(v.PublicKey, ModuleSigningDigest(module), signature) {
		return fmt.Errorf("%w: signature verification failed", ErrModuleSignatureInvalid)
	}
	return nil
}

// ModuleSigningDigest returns the digest signed by Ed25519ModuleVerifier.
// Framing prevents concatenation ambiguity between manifest and definitions.
func ModuleSigningDigest(module Module) []byte {
	hash := sha256.New()
	writeSignatureFrame(hash, module.ManifestBytes)
	for i, definition := range module.Definitions {
		name := ""
		if i < len(module.Manifest.DefinitionFiles) {
			name = module.Manifest.DefinitionFiles[i]
		}
		writeSignatureFrame(hash, []byte(name))
		writeSignatureFrame(hash, definition)
	}
	return hash.Sum(nil)
}

type signatureWriter interface {
	Write([]byte) (int, error)
}

func writeSignatureFrame(w signatureWriter, value []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = w.Write(length[:])
	_, _ = w.Write(value)
}
