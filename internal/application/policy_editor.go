package application

import (
	"context"
	"github.com/bdobrica/ThinkPixelAG/internal/artifact"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"time"
	"unicode/utf8"
)

type SavePolicyDraft struct {
	ExpectedRevision int64  `json:"expected_revision"`
	Source           string `json:"source"`
}
type PromotePolicyDraft struct {
	Revision         int64  `json:"revision"`
	Digest           string `json:"digest"`
	ArtifactRevision uint64 `json:"artifact_revision"`
}
type RequestRollbackApproval struct {
	ExpectedPolicyEpoch int64  `json:"expected_policy_epoch"`
	LifetimeSeconds     int64  `json:"lifetime_seconds"`
	Reason              string `json:"reason_code"`
}

func (s *PolicyAdministration) editor() (ports.PolicyEditorStore, error) {
	store, ok := s.Store.(ports.PolicyEditorStore)
	if !ok {
		return nil, domain.NewError(domain.CodeUnavailable, "policy editor unavailable")
	}
	return store, nil
}
func (s *PolicyAdministration) AuthorizeRead(ctx context.Context, c AdminCaller, resource string) error {
	c.Key = "read-policy-administration"
	_, err := s.Authorize(ctx, c, "policies.manage", resource, nil)
	return err
}
func (s *PolicyAdministration) SaveDraft(ctx context.Context, c AdminCaller, id domain.ID, b SavePolicyDraft) (ports.PolicyDraft, error) {
	if b.ExpectedRevision < 0 || b.ExpectedRevision == 1<<63-1 || id.IsZero() && b.ExpectedRevision != 0 || len(b.Source) == 0 || len(b.Source) > 1<<20 || !utf8.ValidString(b.Source) {
		return ports.PolicyDraft{}, domain.NewError(domain.CodeInvalidArgument, "invalid draft source or revision")
	}
	op, err := s.Authorize(ctx, c, "policies.manage", "draft:"+id.String(), struct {
		ID   string
		Body SavePolicyDraft
	}{id.String(), b})
	if err != nil {
		return ports.PolicyDraft{}, err
	}
	store, err := s.editor()
	if err != nil {
		return ports.PolicyDraft{}, err
	}
	digest, _ := policy.Digest([]byte(b.Source))
	return store.SavePolicyDraft(ctx, op, id, b.ExpectedRevision, b.Source, digest)
}
func (s *PolicyAdministration) Draft(ctx context.Context, c AdminCaller, id domain.ID, revision int64) (ports.PolicyDraft, error) {
	if err := s.AuthorizeRead(ctx, c, id.String()); err != nil {
		return ports.PolicyDraft{}, err
	}
	if revision < 0 {
		return ports.PolicyDraft{}, domain.NewError(domain.CodeInvalidArgument, "invalid revision")
	}
	store, err := s.editor()
	if err != nil {
		return ports.PolicyDraft{}, err
	}
	return store.PolicyDraft(ctx, id, revision)
}
func (s *PolicyAdministration) ValidateDraft(ctx context.Context, c AdminCaller, id domain.ID, revision int64) (map[string]any, error) {
	d, err := s.Draft(ctx, c, id, revision)
	if err != nil {
		return nil, err
	}
	if err = s.Modules.Validate(ctx, []byte(d.Source)); err != nil {
		return nil, domain.NewError(domain.CodeInvalidArgument, "draft compilation failed")
	}
	return map[string]any{"valid": true, "digest": d.Digest, "revision": d.Revision}, nil
}
func (s *PolicyAdministration) PromoteDraft(ctx context.Context, c AdminCaller, id domain.ID, b PromotePolicyDraft) (ports.PolicyArtifact, error) {
	if b.Revision < 1 || b.ArtifactRevision < 1 || b.ArtifactRevision > 1<<63-1 || !domain.ValidDigest(b.Digest) {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeInvalidArgument, "exact draft revision, digest and artifact revision required")
	}
	d, err := s.Draft(ctx, c, id, b.Revision)
	if err != nil {
		return ports.PolicyArtifact{}, err
	}
	if d.Digest != b.Digest {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeConflict, "draft digest mismatch")
	}
	if s.Signer == nil || s.SigningKeyID == "" {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeUnavailable, "policy signer unavailable")
	}
	if err = s.Modules.Validate(ctx, []byte(d.Source)); err != nil {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeInvalidArgument, "draft compilation failed")
	}
	signing, _, err := artifact.SigningDigest(artifact.PolicyBundle, policy.ContractVersion, b.ArtifactRevision, []byte(d.Source))
	if err != nil {
		return ports.PolicyArtifact{}, err
	}
	sig, err := s.Signer.Sign(ctx, s.SigningKeyID, signing)
	if err != nil {
		return ports.PolicyArtifact{}, domain.NewError(domain.CodeUnavailable, "policy signing failed")
	}
	return s.Upload(ctx, c, PolicyUpload{Digest: d.Digest, ContractVersion: policy.ContractVersion, Revision: b.ArtifactRevision, Source: []byte(d.Source), Signature: sig.Value, KeyID: sig.KeyID, KeyVersion: sig.KeyVersion, Algorithm: sig.Algorithm})
}
func (s *PolicyAdministration) RequestRollback(ctx context.Context, c AdminCaller, digest string, b RequestRollbackApproval) (ports.ApprovalView, error) {
	if !domain.ValidDigest(digest) || b.ExpectedPolicyEpoch < 1 || b.LifetimeSeconds < 1 || b.LifetimeSeconds > 3600 || !adminIdentifier.MatchString(b.Reason) {
		return ports.ApprovalView{}, domain.NewError(domain.CodeInvalidArgument, "invalid rollback approval request")
	}
	op, err := s.Authorize(ctx, c, "policies.activate", digest, struct {
		Digest string
		Body   RequestRollbackApproval
	}{digest, b})
	if err != nil {
		return ports.ApprovalView{}, err
	}
	if op.PolicyVersion != b.ExpectedPolicyEpoch {
		return ports.ApprovalView{}, domain.NewError(domain.CodeConflict, "policy activation changed")
	}
	if _, err = s.Store.PolicyArtifact(ctx, s.Channel, digest); err != nil {
		return ports.ApprovalView{}, err
	}
	return s.RequestLocalApproval(ctx, op, domain.ApprovalPolicyRollback, "policy", digest, domain.PolicyActivationDigest(c.TenantID, s.Channel, digest, b.ExpectedPolicyEpoch), b.Reason, time.Duration(b.LifetimeSeconds)*time.Second)
}

// RequestLocalApproval is used by already-authorized, action-specific workflows.
func (s *PolicyAdministration) RequestLocalApproval(ctx context.Context, op ports.AdministrationOperation, action domain.GovernanceApprovalAction, resourceType, resource, digest, reason string, lifetime time.Duration) (ports.ApprovalView, error) {
	if s.ApprovalProvider == nil {
		return ports.ApprovalView{}, domain.NewError(domain.CodeUnavailable, "approval provider unavailable")
	}
	store, err := s.editor()
	if err != nil {
		return ports.ApprovalView{}, err
	}
	id, err := domain.NewID()
	if err != nil {
		return ports.ApprovalView{}, err
	}
	a := domain.GovernanceApproval{ID: id, TenantID: op.TenantID, RequesterPrincipalID: op.ActorID, Action: action, ResourceType: resourceType, ResourceID: resource, RequestDigest: digest, ReasonCode: reason, Provider: "local-oidc", State: domain.GovernanceApprovalPending, RequestedAt: op.At, ExpiresAt: op.At.Add(lifetime)}
	a.ProviderReference, err = s.ApprovalProvider.RequestApproval(ctx, ports.ApprovalChallenge{ApprovalID: id, TenantID: op.TenantID, RequesterPrincipalID: op.ActorID, Action: action, ResourceType: resourceType, ResourceID: resource, RequestDigest: digest, ExpiresAt: a.ExpiresAt})
	if err != nil {
		return ports.ApprovalView{}, domain.NewError(domain.CodeUnavailable, "approval provider unavailable")
	}
	if err = a.Validate(); err != nil {
		return ports.ApprovalView{}, err
	}
	return store.CreateLocalApproval(ctx, op, a)
}
func (s *PolicyAdministration) Approval(ctx context.Context, c AdminCaller, id domain.ID) (ports.ApprovalView, error) {
	if err := s.AuthorizeRead(ctx, c, id.String()); err != nil {
		return ports.ApprovalView{}, err
	}
	store, err := s.editor()
	if err != nil {
		return ports.ApprovalView{}, err
	}
	a, err := store.GovernanceApproval(ctx, id)
	if err != nil {
		return ports.ApprovalView{}, err
	}
	return ports.ApprovalProjection(a, s.Clock.Now()), nil
}
func (s *PolicyAdministration) DecideApproval(ctx context.Context, c AdminCaller, id domain.ID, approved bool) (ports.ApprovalView, error) {
	store, err := s.editor()
	if err != nil {
		return ports.ApprovalView{}, err
	}
	a, err := store.GovernanceApproval(ctx, id)
	if err != nil {
		return ports.ApprovalView{}, err
	}
	action := "policies.activate"
	if a.Action == domain.ApprovalEmergencyExpansion {
		action = "role_mappings.manage"
		allowed := false
		for _, role := range c.Roles {
			allowed = allowed || role == "policy-admin"
		}
		if !allowed {
			return ports.ApprovalView{}, domain.NewError(domain.CodeForbidden, "policy-administrator required")
		}
	} else if a.Action != domain.ApprovalPolicyRollback {
		return ports.ApprovalView{}, domain.NewError(domain.CodeForbidden, "unsupported local approval action")
	}
	op, err := s.Authorize(ctx, c, action, id.String(), struct {
		ID       domain.ID
		Approved bool
	}{id, approved})
	if err != nil {
		return ports.ApprovalView{}, err
	}
	return store.DecideLocalApproval(ctx, op, id, approved)
}
