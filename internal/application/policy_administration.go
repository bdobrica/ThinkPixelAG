package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"regexp"
)

var adminKey = regexp.MustCompile(`^[A-Za-z0-9._:-]{16,128}$`)
var adminIdentifier = regexp.MustCompile(`^[a-zA-Z0-9._:-]{1,128}$`)

type PolicyAdministration struct {
	Store     ports.PolicyAdministrationStore
	Evaluator policy.Evaluator
	Modules   ports.PolicyModuleRuntime
	Verifier  ports.Verifier
	Channel   string
	Clock     domain.Clock
}
type AdminCaller struct {
	TenantID, PrincipalID, RequestID domain.ID
	Roles                            []string
	Key                              string
}
type PolicyUpload struct {
	Digest          string                   `json:"digest"`
	ContractVersion string                   `json:"contract_version"`
	Revision        uint64                   `json:"artifact_revision"`
	Source          []byte                   `json:"artifact"`
	Signature       []byte                   `json:"signature"`
	KeyID           string                   `json:"signing_key_id"`
	KeyVersion      string                   `json:"signing_key_version"`
	Algorithm       ports.SignatureAlgorithm `json:"signature_algorithm"`
}
type PolicyActivate struct {
	Channel  string `json:"channel"`
	Reason   string `json:"reason_code"`
	Approval string `json:"approval_reference"`
}

func (s *PolicyAdministration) Authorize(ctx context.Context, c AdminCaller, action, resource string, body any) (ports.AdministrationOperation, error) {
	if s.Store == nil || s.Evaluator == nil || s.Clock == nil || c.TenantID.IsZero() || c.PrincipalID.IsZero() || c.RequestID.IsZero() {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeUnauthenticated, "administration requires verified identity")
	}
	if !adminKey.MatchString(c.Key) {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeInvalidArgument, "valid Idempotency-Key required")
	}
	id, err := domain.NewID()
	if err != nil {
		return ports.AdministrationOperation{}, err
	}
	in := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: id.String(), RequestTime: s.Clock.Now(), Subject: policy.Subject{PrincipalID: c.PrincipalID.String(), TenantID: c.TenantID.String(), PrincipalType: "human", Roles: c.Roles}, Action: action, Resource: policy.Resource{Type: "policy", ID: resource, TenantID: c.TenantID.String(), Attributes: map[string]any{}}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: policy.SecurityState{Authoritative: true}, Context: policy.RequestContext{RequestID: c.RequestID.String()}}
	result, err := s.Evaluator.Decide(ctx, in)
	if err != nil {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeUnavailable, "administration policy unavailable").WithRetryable()
	}
	if !result.Decision.Allow {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeForbidden, "administration not permitted")
	}
	if result.Decision.DecisionID != id.String() || !domain.ValidDigest(result.Metadata.PolicyDigest) || result.Metadata.PolicyVersion < 1 {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeUnavailable, "invalid administration policy evidence")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ports.AdministrationOperation{}, err
	}
	sum := sha256.Sum256(raw)
	return ports.AdministrationOperation{TenantID: c.TenantID, ActorID: c.PrincipalID, RequestID: c.RequestID, DecisionID: id, Action: action, Resource: resource, Channel: s.Channel, Key: c.Key, RequestHash: "sha256:" + hex.EncodeToString(sum[:]), PolicyDigest: result.Metadata.PolicyDigest, PolicyVersion: result.Metadata.PolicyVersion, At: s.Clock.Now()}, nil
}
func (s *PolicyAdministration) Upload(ctx context.Context, c AdminCaller, b PolicyUpload) (ports.PolicyArtifact, error) {
	op, err := s.Authorize(ctx, c, "policies.manage", b.Digest, b)
	if err != nil {
		return ports.PolicyArtifact{}, err
	}
	if s.Verifier == nil || s.Modules == nil {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeUnavailable, "policy promotion adapters unavailable")
	}
	sig := ports.Signature{KeyID: b.KeyID, KeyVersion: b.KeyVersion, Algorithm: b.Algorithm, Value: b.Signature}
	if len(b.Source) > 1<<20 || b.Revision > uint64(1<<63-1) {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeInvalidArgument, "policy artifact exceeds bounds")
	}
	if err := policy.VerifyBundle(ctx, b.Revision, b.ContractVersion, b.Digest, b.Source, sig, s.Verifier, s.Modules); err != nil {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeInvalidArgument, "policy signature, contract or compilation invalid")
	}
	id, err := domain.NewID()
	if err != nil {
		return ports.PolicyArtifact{}, err
	}
	return s.Store.StorePolicy(ctx, op, ports.PolicyArtifact{ID: id, Digest: b.Digest, ContractVersion: b.ContractVersion, Revision: b.Revision, Source: b.Source, Signature: sig, Channel: s.Channel, State: "VALIDATED", CreatedAt: op.At})
}
func (s *PolicyAdministration) Activate(ctx context.Context, c AdminCaller, digest string, b PolicyActivate) (ports.PolicyActivation, error) {
	if !domain.ValidDigest(digest) || b.Channel != s.Channel || !adminIdentifier.MatchString(b.Reason) || len(b.Approval) < 1 || len(b.Approval) > 256 {
		return ports.PolicyActivation{}, domain.NewError(domain.CodeInvalidArgument, "invalid policy activation")
	}
	op, err := s.Authorize(ctx, c, "policies.activate", digest, struct {
		Digest string
		Body   PolicyActivate
	}{digest, b})
	if err != nil {
		return ports.PolicyActivation{}, err
	}
	target, err := s.Store.PolicyArtifact(ctx, s.Channel, digest)
	if err != nil {
		return ports.PolicyActivation{}, err
	}
	if s.Verifier == nil || s.Modules == nil {
		return ports.PolicyActivation{}, domain.NewError(domain.CodeUnavailable, "policy promotion adapters unavailable")
	}
	if err := artifact.Verify(ctx, artifact.Envelope{Kind: artifact.PolicyBundle, FormatVersion: target.ContractVersion, Revision: target.Revision, Digest: target.Digest, Payload: target.Source, Signature: target.Signature}, s.Verifier, artifact.Versions{artifact.PolicyBundle: {policy.ContractVersion: {}}}); err != nil {
		return ports.PolicyActivation{}, domain.NewError(domain.CodeUnavailable, "stored policy signature invalid")
	}
	if err := s.Modules.Ensure(ctx, target.Digest, target.Source); err != nil {
		return ports.PolicyActivation{}, domain.NewError(domain.CodeUnavailable, "policy module unavailable").WithRetryable()
	}
	return s.Store.ActivatePolicy(ctx, op, b.Reason, b.Approval)
}
