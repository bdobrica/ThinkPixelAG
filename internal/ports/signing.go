package ports

import "context"

type SignatureAlgorithm string

const (
	SignatureEd25519      SignatureAlgorithm = "ED25519"
	SignatureECDSASHA256  SignatureAlgorithm = "ECDSA_SHA256"
	SignatureRSAPSSSHA256 SignatureAlgorithm = "RSA_PSS_SHA256"
)

func (algorithm SignatureAlgorithm) Valid() bool {
	switch algorithm {
	case SignatureEd25519, SignatureECDSASHA256, SignatureRSAPSSSHA256:
		return true
	default:
		return false
	}
}

type KeyProtection string

const (
	KeyProtectionSoftware KeyProtection = "SOFTWARE"
	KeyProtectionKMS      KeyProtection = "KMS"
	KeyProtectionHSM      KeyProtection = "HSM"
)

type SigningKey struct {
	ID, Version string
	Algorithm   SignatureAlgorithm
	Protection  KeyProtection
	Enabled     bool
	Exportable  bool
	CanSign     bool
	CanVerify   bool
}

// KeyInspector returns provider-authoritative metadata without exposing key
// material. Managed adapters implement it using their KMS/HSM describe API.
type KeyInspector interface {
	DescribeKey(context.Context, string) (SigningKey, error)
}

type Signature struct {
	KeyID, KeyVersion string
	Algorithm         SignatureAlgorithm
	Value             []byte
}

type SigningDigest struct {
	Hash  string
	Value []byte
}

// Signer is compatible with remote KMS/HSM calls: callers pass a digest and
// receive only signature metadata and bytes, never a private key.
type Signer interface {
	Sign(context.Context, string, SigningDigest) (Signature, error)
}

type Verifier interface {
	Verify(context.Context, Signature, SigningDigest) error
}
