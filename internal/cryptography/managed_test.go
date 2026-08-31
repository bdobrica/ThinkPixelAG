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
