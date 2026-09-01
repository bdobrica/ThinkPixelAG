// Package artifact verifies versioned, privileged governance artifacts.
package artifact

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const MaxPayloadBytes = 4 << 20

type Kind string

const (
	PolicyBundle         Kind = "POLICY_BUNDLE"
	TrustRootSet         Kind = "TRUST_ROOT_SET"
	RevocationConfig     Kind = "REVOCATION_CONFIG"
	ResourceOverride     Kind = "RESOURCE_OVERRIDE_CONFIG"
	PrivilegedAgentClass Kind = "PRIVILEGED_AGENT_CLASS_CONFIG"
)

func (kind Kind) Valid() bool {
	switch kind {
	case PolicyBundle, TrustRootSet, RevocationConfig, ResourceOverride, PrivilegedAgentClass:
		return true
	default:
		return false
	}
}

// Envelope contains all identity metadata covered by the signature. Revision
// is monotonic within the artifact owner's scope; FormatVersion identifies the
// payload schema, not the activation version.
type Envelope struct {
	Kind          Kind
	FormatVersion string
	Revision      uint64
	Digest        string
	Payload       []byte
	Signature     ports.Signature
}

type VersionPolicy interface {
	Accepts(Kind, string) bool
}

type Versions map[Kind]map[string]struct{}

func (versions Versions) Accepts(kind Kind, version string) bool {
	_, ok := versions[kind][version]
	return ok
}

// Verify recalculates the payload digest and verifies a domain-separated,
// deterministic manifest. Thus substituting kind, version, revision, digest,
// key version, or algorithm invalidates the signature.
func Verify(ctx context.Context, envelope Envelope, verifier ports.Verifier, versions VersionPolicy) error {
	if !envelope.Kind.Valid() || envelope.FormatVersion == "" || strings.TrimSpace(envelope.FormatVersion) != envelope.FormatVersion || len(envelope.FormatVersion) > 128 || envelope.Revision == 0 || envelope.Revision > math.MaxInt64 {
		return errors.New("signed artifact identity or version is invalid")
	}
	if len(envelope.Payload) == 0 || len(envelope.Payload) > MaxPayloadBytes {
		return errors.New("signed artifact payload size is outside bounds")
	}
	if verifier == nil || versions == nil || !versions.Accepts(envelope.Kind, envelope.FormatVersion) {
		return errors.New("signed artifact format version is unsupported")
	}
	sum := sha256.Sum256(envelope.Payload)
	want := "sha256:" + hex.EncodeToString(sum[:])
	if len(envelope.Digest) != len(want) || subtle.ConstantTimeCompare([]byte(envelope.Digest), []byte(want)) != 1 {
		return errors.New("signed artifact digest mismatch")
	}
	manifest := signingManifest(envelope.Kind, envelope.FormatVersion, envelope.Revision, sum)
	manifestDigest := sha256.Sum256(manifest)
	if err := verifier.Verify(ctx, envelope.Signature, ports.SigningDigest{Hash: "SHA-256", Value: manifestDigest[:]}); err != nil {
		return errors.New("signed artifact signature verification failed")
	}
	return nil
}

func signingManifest(kind Kind, version string, revision uint64, payloadDigest [sha256.Size]byte) []byte {
	// Length prefixes avoid delimiter ambiguity. The fixed prefix prevents a
	// signature over another protocol's digest from being replayed here.
	manifest := []byte("thinkpixelag.signed-artifact/v1\x00")
	for _, value := range [][]byte{[]byte(kind), []byte(version), payloadDigest[:]} {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		manifest = append(manifest, size[:]...)
		manifest = append(manifest, value...)
	}
	var encodedRevision [8]byte
	binary.BigEndian.PutUint64(encodedRevision[:], revision)
	return append(manifest, encodedRevision[:]...)
}

// SigningDigest returns the exact digest a managed signer must sign.
func SigningDigest(kind Kind, version string, revision uint64, payload []byte) (ports.SigningDigest, string, error) {
	if !kind.Valid() || version == "" || revision == 0 || revision > math.MaxInt64 || len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return ports.SigningDigest{}, "", errors.New("signed artifact is invalid")
	}
	payloadDigest := sha256.Sum256(payload)
	manifestDigest := sha256.Sum256(signingManifest(kind, version, revision, payloadDigest))
	return ports.SigningDigest{Hash: "SHA-256", Value: manifestDigest[:]}, "sha256:" + hex.EncodeToString(payloadDigest[:]), nil
}
