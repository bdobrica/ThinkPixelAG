package policy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

func validInput() Input {
	return Input{ContractVersion: ContractVersion, DecisionID: "d", RequestTime: time.Now().UTC(), Subject: Subject{PrincipalID: "p", TenantID: "t", PrincipalType: "human", Roles: []string{"agent-invoker"}}, Action: "runs.create", Resource: Resource{Type: "agent", ID: "a", TenantID: "t", Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{"tokens": float64(100), "tools": []any{"a", "b"}}, SecurityState: SecurityState{}, Context: RequestContext{RequestID: "r"}}
}
func TestDecisionRejectsExpansionAndUnknownReason(t *testing.T) {
	in := validInput()
	d := Decision{ContractVersion: ContractVersion, DecisionID: "d", Allow: true, ReasonCodes: []string{"agent.invoke.allowed"}, ResolvedConstraints: map[string]any{"tokens": float64(101)}, Obligations: []Obligation{}, DecisionTTLSeconds: 1}
	if ValidateDecision(d, in, time.Second) == nil {
		t.Fatal("accepted expansion")
	}
	d.ResolvedConstraints = map[string]any{"tokens": float64(99)}
	d.ReasonCodes = []string{"secret.reason"}
	if ValidateDecision(d, in, time.Second) == nil {
		t.Fatal("accepted unknown reason")
	}
}

type acceptValidator struct{}

func (acceptValidator) Validate(context.Context, []byte) error { return nil }

type policyTestVerifier struct{ key ed25519.PublicKey }

func (v policyTestVerifier) Verify(_ context.Context, signature ports.Signature, digest ports.SigningDigest) error {
	if signature.KeyID != "key" || signature.KeyVersion != "1" || signature.Algorithm != ports.SignatureEd25519 || !ed25519.Verify(v.key, digest.Value, signature.Value) {
		return errors.New("invalid signature")
	}
	return nil
}

func TestBundleDigestAndSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body := []byte("bundle")
	signingDigest, digest, _ := artifact.SigningDigest(artifact.PolicyBundle, ContractVersion, 1, body)
	sig := ports.Signature{KeyID: "key", KeyVersion: "1", Algorithm: ports.SignatureEd25519, Value: ed25519.Sign(priv, signingDigest.Value)}
	if err := VerifyBundle(context.Background(), 1, ContractVersion, digest, body, sig, policyTestVerifier{key: pub}, acceptValidator{}); err != nil {
		t.Fatal(err)
	}
	body[0] = 'B'
	if VerifyBundle(context.Background(), 1, ContractVersion, digest, body, sig, policyTestVerifier{key: pub}, acceptValidator{}) == nil {
		t.Fatal("accepted changed bundle")
	}
}
func TestFreshnessIsMonotonicAndBounded(t *testing.T) {
	now := time.Now().UTC()
	f, _ := NewFreshness(time.Minute, func() time.Time { return now })
	a := ActiveBundle{TenantID: "t", Channel: "stable", Digest: "sha256:x", Version: 1, ActivatedAt: now}
	if err := f.Set(a); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Get("t", "stable"); !ok {
		t.Fatal("not fresh")
	}
	now = now.Add(30 * time.Second)
	if err := f.Set(a); err != nil {
		t.Fatal("rejected identical freshness refresh")
	}
	now = now.Add(45 * time.Second)
	if _, ok := f.Get("t", "stable"); !ok {
		t.Fatal("refreshed policy became stale too early")
	}
	now = now.Add(30 * time.Second)
	if _, ok := f.Get("t", "stable"); ok {
		t.Fatal("stale policy reported fresh")
	}
}
