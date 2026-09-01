package artifact

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type digestBoundVerifier struct{}

func (digestBoundVerifier) Verify(_ context.Context, signature ports.Signature, digest ports.SigningDigest) error {
	if signature.KeyID != "key" || signature.KeyVersion != "7" || signature.Algorithm != ports.SignatureEd25519 || !bytes.Equal(signature.Value, digest.Value) {
		return errors.New("signature mismatch")
	}
	return nil
}

func signedEnvelope(t *testing.T, kind Kind, version string, revision uint64, payload []byte) Envelope {
	t.Helper()
	digest, contentDigest, err := SigningDigest(kind, version, revision, payload)
	if err != nil {
		t.Fatal(err)
	}
	return Envelope{Kind: kind, FormatVersion: version, Revision: revision, Digest: contentDigest, Payload: payload,
		Signature: ports.Signature{KeyID: "key", KeyVersion: "7", Algorithm: ports.SignatureEd25519, Value: digest.Value}}
}

func TestVerifyBindsAllArtifactIdentity(t *testing.T) {
	versions := Versions{PolicyBundle: {"thinkpixelag.authorization/v1alpha1": {}}, TrustRootSet: {"thinkpixelag.trust-roots/v1": {}}}
	original := signedEnvelope(t, PolicyBundle, "thinkpixelag.authorization/v1alpha1", 3, []byte("package authorization"))
	if err := Verify(context.Background(), original, digestBoundVerifier{}, versions); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*Envelope){
		"payload":     func(e *Envelope) { e.Payload[0] ^= 1 },
		"digest":      func(e *Envelope) { e.Digest = "sha256:" + string(make([]byte, 64)) },
		"kind":        func(e *Envelope) { e.Kind = TrustRootSet },
		"version":     func(e *Envelope) { e.FormatVersion = "thinkpixelag.trust-roots/v1" },
		"revision":    func(e *Envelope) { e.Revision++ },
		"key version": func(e *Envelope) { e.Signature.KeyVersion = "8" },
		"algorithm":   func(e *Envelope) { e.Signature.Algorithm = ports.SignatureECDSASHA256 },
		"signature":   func(e *Envelope) { e.Signature.Value[0] ^= 1 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := original
			candidate.Payload = append([]byte(nil), original.Payload...)
			candidate.Signature.Value = append([]byte(nil), original.Signature.Value...)
			mutate(&candidate)
			if err := Verify(context.Background(), candidate, digestBoundVerifier{}, versions); err == nil {
				t.Fatal("mismatched artifact accepted")
			}
		})
	}
}

func TestVerifyRejectsUnknownKindAndVersion(t *testing.T) {
	versions := Versions{PolicyBundle: {"thinkpixelag.authorization/v1alpha1": {}}}
	for _, envelope := range []Envelope{
		signedEnvelope(t, RevocationConfig, "thinkpixelag.revocation-config/v1", 1, []byte("{}")),
		{Kind: "UNKNOWN", FormatVersion: "v1", Revision: 1, Digest: "sha256:x", Payload: []byte("x")},
	} {
		if err := Verify(context.Background(), envelope, digestBoundVerifier{}, versions); err == nil {
			t.Fatal("unsupported artifact accepted")
		}
	}
}
