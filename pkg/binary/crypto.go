package binary

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// KeyResolver returns a symmetric key by logical key id.
type KeyResolver func(ctx context.Context, keyID string) ([]byte, error)

func encryptBytes(ctx context.Context, data []byte, enc *BlobEncryption, resolveKey KeyResolver) ([]byte, *BlobEncryption, error) {
	if enc == nil || enc.Algorithm == EncryptionNone {
		return data, nil, nil
	}
	if enc.Algorithm != EncryptionAES256GCM {
		return nil, nil, fmt.Errorf("%w: unsupported encryption algorithm %q", ErrUnsupported, enc.Algorithm)
	}
	if resolveKey == nil {
		return nil, nil, fmt.Errorf("%w: key resolver is required", ErrInvalidArgument)
	}
	key, err := resolveKey(ctx, enc.KeyID)
	if err != nil {
		return nil, nil, err
	}
	if len(key) != 32 {
		return nil, nil, fmt.Errorf("%w: aes256-gcm requires 32-byte key", ErrInvalidArgument)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	out := gcm.Seal(nil, nonce, data, nil)
	return out, &BlobEncryption{
		Algorithm: EncryptionAES256GCM,
		KeyID:     enc.KeyID,
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
	}, nil
}

func decryptBytes(ctx context.Context, data []byte, enc *BlobEncryption, resolveKey KeyResolver) ([]byte, error) {
	if enc == nil || enc.Algorithm == EncryptionNone {
		return data, nil
	}
	if enc.Algorithm != EncryptionAES256GCM {
		return nil, fmt.Errorf("%w: unsupported encryption algorithm %q", ErrUnsupported, enc.Algorithm)
	}
	if resolveKey == nil {
		return nil, fmt.Errorf("%w: key resolver is required", ErrInvalidArgument)
	}
	key, err := resolveKey(ctx, enc.KeyID)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: aes256-gcm requires 32-byte key", ErrInvalidArgument)
	}
	nonce, err := base64.StdEncoding.DecodeString(enc.Nonce)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, data, nil)
}
