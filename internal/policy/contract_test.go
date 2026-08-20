package policy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
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
func TestBundleDigestAndSignature(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body := []byte("bundle")
	digest, _ := Digest(body)
	sig := ed25519.Sign(priv, body)
	if err := VerifyBundle(context.Background(), digest, "key", body, sig, Ed25519Verifier{Keys: map[string]ed25519.PublicKey{"key": pub}}, acceptValidator{}); err != nil {
		t.Fatal(err)
	}
	body[0] = 'B'
	if VerifyBundle(context.Background(), digest, "key", body, sig, Ed25519Verifier{Keys: map[string]ed25519.PublicKey{"key": pub}}, acceptValidator{}) == nil {
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
