// Package cryptography enforces provider-independent managed-key invariants.
package cryptography

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maxSignatureBytes = 16 << 10

type ManagedSigner struct {
	backend ports.Signer
	key     ports.SigningKey
}

var _ ports.Signer = (*ManagedSigner)(nil)

func NewManagedSigner(ctx context.Context, keyID string, algorithm ports.SignatureAlgorithm, backend ports.Signer, inspector ports.KeyInspector) (*ManagedSigner, error) {
	if keyID == "" || !algorithm.Valid() || backend == nil || inspector == nil {
		return nil, errors.New("managed signer requires a key identifier, backend, and inspector")
	}
	key, err := inspector.DescribeKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("describe managed signing key: %w", err)
	}
	if err := validateManagedKey(keyID, key, true); err != nil {
		return nil, err
	}
	if key.Algorithm != algorithm {
		return nil, errors.New("managed signing key algorithm does not match configuration")
	}
	return &ManagedSigner{backend: backend, key: key}, nil
}

func (signer *ManagedSigner) Sign(ctx context.Context, keyID string, digest ports.SigningDigest) (ports.Signature, error) {
	if signer == nil || keyID != signer.key.ID || !validDigest(digest) {
		return ports.Signature{}, errors.New("managed signing request is invalid")
	}
	signature, err := signer.backend.Sign(ctx, keyID, digest)
	if err != nil {
		return ports.Signature{}, fmt.Errorf("managed signing operation failed: %w", err)
	}
	if signature.KeyID != signer.key.ID || signature.KeyVersion != signer.key.Version || signature.Algorithm != signer.key.Algorithm || len(signature.Value) == 0 || len(signature.Value) > maxSignatureBytes {
		return ports.Signature{}, errors.New("managed signer returned mismatched signature metadata")
	}
	return signature, nil
}

type ManagedVerifier struct {
	backend ports.Verifier
	key     ports.SigningKey
}

var _ ports.Verifier = (*ManagedVerifier)(nil)

func NewManagedVerifier(ctx context.Context, keyID string, algorithm ports.SignatureAlgorithm, backend ports.Verifier, inspector ports.KeyInspector) (*ManagedVerifier, error) {
	if keyID == "" || !algorithm.Valid() || backend == nil || inspector == nil {
		return nil, errors.New("managed verifier requires a key identifier, backend, and inspector")
	}
	key, err := inspector.DescribeKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("describe managed verification key: %w", err)
	}
	if err := validateManagedKey(keyID, key, false); err != nil {
		return nil, err
	}
	if key.Algorithm != algorithm {
		return nil, errors.New("managed verification key algorithm does not match configuration")
	}
	return &ManagedVerifier{backend: backend, key: key}, nil
}

func (verifier *ManagedVerifier) Verify(ctx context.Context, signature ports.Signature, digest ports.SigningDigest) error {
	if verifier == nil || signature.KeyID != verifier.key.ID || signature.KeyVersion != verifier.key.Version || signature.Algorithm != verifier.key.Algorithm || len(signature.Value) == 0 || len(signature.Value) > maxSignatureBytes || !validDigest(digest) {
		return errors.New("managed verification request is invalid")
	}
	if err := verifier.backend.Verify(ctx, signature, digest); err != nil {
		return fmt.Errorf("managed signature verification failed: %w", err)
	}
	return nil
}

func validDigest(digest ports.SigningDigest) bool {
	return digest.Hash == "SHA-256" && len(digest.Value) == 32
}

func validateManagedKey(requestedID string, key ports.SigningKey, signing bool) error {
	if key.ID != requestedID || len(key.ID) > 512 || key.Version == "" || len(key.Version) > 256 ||
		strings.TrimSpace(key.Version) != key.Version || !key.Algorithm.Valid() || !key.Enabled {
		return errors.New("managed key metadata is invalid or disabled")
	}
	if key.Exportable || (key.Protection != ports.KeyProtectionKMS && key.Protection != ports.KeyProtectionHSM) {
		return errors.New("production signing keys must be non-exportable KMS/HSM keys")
	}
	if (signing && !key.CanSign) || (!signing && !key.CanVerify) {
		return errors.New("managed key does not permit the requested operation")
	}
	return nil
}
