package cryptography

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type keyBackendStub struct {
	key       ports.SigningKey
	signature ports.Signature
	err       error
}

func (stub keyBackendStub) DescribeKey(context.Context, string) (ports.SigningKey, error) {
	return stub.key, stub.err
}
func (stub keyBackendStub) Sign(context.Context, string, ports.SigningDigest) (ports.Signature, error) {
	return stub.signature, stub.err
}
func (stub keyBackendStub) Verify(context.Context, ports.Signature, ports.SigningDigest) error {
	return stub.err
}

func testDigest() ports.SigningDigest {
	return ports.SigningDigest{Hash: "SHA-256", Value: make([]byte, 32)}
}

func managedTestKey() ports.SigningKey {
	return ports.SigningKey{ID: "kms://keys/policy", Version: "7", Algorithm: ports.SignatureEd25519, Protection: ports.KeyProtectionHSM, Enabled: true, CanSign: true, CanVerify: true}
}

func TestManagedSignerAcceptsOnlyBoundNonExportableKeys(t *testing.T) {
	key := managedTestKey()
	backend := keyBackendStub{key: key, signature: ports.Signature{KeyID: key.ID, KeyVersion: key.Version, Algorithm: key.Algorithm, Value: []byte("signature")}}
	signer, err := NewManagedSigner(context.Background(), key.ID, key.Algorithm, backend, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), key.ID, testDigest()); err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []func(*ports.SigningKey){
		func(key *ports.SigningKey) { key.Exportable = true },
		func(key *ports.SigningKey) { key.Protection = ports.KeyProtectionSoftware },
		func(key *ports.SigningKey) { key.CanSign = false },
		func(key *ports.SigningKey) { key.Enabled = false },
		func(key *ports.SigningKey) { key.Algorithm = ports.SignatureECDSASHA256 },
	} {
		candidate := key
		mutate(&candidate)
		bad := keyBackendStub{key: candidate}
		if _, err := NewManagedSigner(context.Background(), key.ID, key.Algorithm, bad, bad); err == nil {
			t.Fatalf("accepted unsafe key: %#v", candidate)
		}
	}
}

func TestManagedSignerRejectsProviderMetadataSubstitutionAndFailure(t *testing.T) {
	key := managedTestKey()
	backend := keyBackendStub{key: key, signature: ports.Signature{KeyID: key.ID, KeyVersion: "8", Algorithm: key.Algorithm, Value: []byte("signature")}}
	signer, _ := NewManagedSigner(context.Background(), key.ID, key.Algorithm, backend, backend)
	if _, err := signer.Sign(context.Background(), key.ID, testDigest()); err == nil {
		t.Fatal("accepted substituted key version")
	}
	backend.signature.KeyVersion = key.Version
	backend.err = errors.New("kms unavailable")
	signer, _ = NewManagedSigner(context.Background(), key.ID, key.Algorithm, keyBackendStub{key: key}, keyBackendStub{key: key})
	signer.backend = backend
	if _, err := signer.Sign(context.Background(), key.ID, testDigest()); err == nil {
		t.Fatal("accepted KMS failure")
	}
}

func TestManagedVerifierBindsSignatureMetadata(t *testing.T) {
	key := managedTestKey()
	backend := keyBackendStub{key: key}
	verifier, err := NewManagedVerifier(context.Background(), key.ID, key.Algorithm, backend, backend)
	if err != nil {
		t.Fatal(err)
	}
	signature := ports.Signature{KeyID: key.ID, KeyVersion: key.Version, Algorithm: key.Algorithm, Value: []byte("signature")}
	if err := verifier.Verify(context.Background(), signature, testDigest()); err != nil {
		t.Fatal(err)
	}
	signature.Algorithm = ports.SignatureRSAPSSSHA256
	if err := verifier.Verify(context.Background(), signature, testDigest()); err == nil {
		t.Fatal("accepted algorithm substitution")
	}
}

// This is a provider-contract rehearsal, not a real KMS rotation. The backend
// simulates an alias advancing to another immutable version and key retirement.
func TestManagedKeyRotationKeepsVersionsBoundAndFailsClosed(t *testing.T) {
	ctx := context.Background()
	key := managedTestKey()
	oldBackend := &keyBackendStub{key: key, signature: ports.Signature{KeyID: key.ID, KeyVersion: key.Version, Algorithm: key.Algorithm, Value: []byte("old-signature")}}
	oldSigner, err := NewManagedSigner(ctx, key.ID, key.Algorithm, oldBackend, oldBackend)
	if err != nil {
		t.Fatal(err)
	}
	oldVerifier, err := NewManagedVerifier(ctx, key.ID, key.Algorithm, oldBackend, oldBackend)
	if err != nil {
		t.Fatal(err)
	}
	oldSignature, err := oldSigner.Sign(ctx, key.ID, testDigest())
	if err != nil {
		t.Fatal(err)
	}

	// Existing instances retain the inspected version; moving a provider alias
	// must not silently grant them authority to sign with the new version.
	newKey := key
	newKey.Version = "8"
	newSignature := ports.Signature{KeyID: key.ID, KeyVersion: newKey.Version, Algorithm: key.Algorithm, Value: []byte("new-signature")}
	oldBackend.signature = newSignature
	if _, err := oldSigner.Sign(ctx, key.ID, testDigest()); err == nil {
		t.Fatal("old signer accepted the rotated alias version")
	}
	newBackend := &keyBackendStub{key: newKey, signature: newSignature}
	newSigner, err := NewManagedSigner(ctx, key.ID, key.Algorithm, newBackend, newBackend)
	if err != nil {
		t.Fatal(err)
	}
	newVerifier, err := NewManagedVerifier(ctx, key.ID, key.Algorithm, newBackend, newBackend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newSigner.Sign(ctx, key.ID, testDigest()); err != nil {
		t.Fatal(err)
	}
	// During overlap, the caller selects an explicitly bound verifier. Neither
	// verifier accepts another version merely because the key ID is unchanged.
	if err := oldVerifier.Verify(ctx, oldSignature, testDigest()); err != nil {
		t.Fatal(err)
	}
	if err := newVerifier.Verify(ctx, newSignature, testDigest()); err != nil {
		t.Fatal(err)
	}
	if oldVerifier.Verify(ctx, newSignature, testDigest()) == nil || newVerifier.Verify(ctx, oldSignature, testDigest()) == nil {
		t.Fatal("verification crossed immutable key versions")
	}
	oldBackend.err = errors.New("old key retired by provider")
	if oldVerifier.Verify(ctx, oldSignature, testDigest()) == nil {
		t.Fatal("retired key still verified")
	}
	newBackend.err = errors.New("provider unavailable")
	if _, err := newSigner.Sign(ctx, key.ID, testDigest()); err == nil {
		t.Fatal("provider outage allowed signing")
	}
	if newVerifier.Verify(ctx, newSignature, testDigest()) == nil {
		t.Fatal("provider outage allowed verification")
	}
	newBackend.err = nil
	if err := newVerifier.Verify(ctx, newSignature, testDigest()); err != nil {
		t.Fatal(err)
	}
}
