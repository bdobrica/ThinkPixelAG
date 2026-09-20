package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/identity"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"time"
)

type RecoveryStore interface {
	ports.PolicyAdministrationStore
	ports.PolicyEditorStore
	ports.RoleMappingStore
	ports.AgentRegistry
}

// OperatorRecovery deliberately uses local deployment authority, not an invented
// OIDC recovery role. Composition is restricted to the offline operator binary;
// the actor and independent approver must still authenticate to the pinned IdP.
type OperatorRecovery struct {
	Store           RecoveryStore
	Issuer, Channel string
	Clock           domain.Clock
	Provider        ports.ApprovalProvider
}

func (s *OperatorRecovery) operation(ctx context.Context, p identity.Principal, key, action string, body any) (ports.AdministrationOperation, error) {
	tenant, e1 := domain.ParseID(p.TenantID)
	actor, e2 := domain.ParseID(p.ID)
	if e1 != nil || e2 != nil || p.Issuer != s.Issuer || !adminKey.MatchString(key) {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeUnauthenticated, "verified recovery identity and replay key required")
	}
	eligibility, e := s.Store.PrincipalEligibility(ctx, actor)
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	if !eligibility.Exists || eligibility.Disabled {
		return ports.AdministrationOperation{}, domain.NewError(domain.CodeForbidden, "active provisioned operator identity required")
	}
	_, active, e := s.Store.CurrentPolicy(ctx, s.Channel)
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	m, e := s.Store.RoleMappings(ctx, s.Issuer)
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	id, e := domain.NewID()
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	request, e := domain.NewID()
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	raw, e := json.Marshal(body)
	if e != nil {
		return ports.AdministrationOperation{}, e
	}
	sum := sha256.Sum256(raw)
	return ports.AdministrationOperation{TenantID: tenant, ActorID: actor, DecisionID: id, RequestID: request, Action: action, Resource: "role-mappings", Channel: s.Channel, Key: key, RequestHash: "sha256:" + hex.EncodeToString(sum[:]), PolicyDigest: active.Digest, PolicyVersion: active.Version, MappingIssuer: s.Issuer, MappingRevision: m.Revision, At: s.Clock.Now().UTC()}, nil
}
func (s *OperatorRecovery) Request(ctx context.Context, p identity.Principal, key string, b UpdateRoleMappings) (ports.ApprovalView, error) {
	op, e := s.operation(ctx, p, key, "operator.role_mappings.recovery.request", b)
	if e != nil {
		return ports.ApprovalView{}, e
	}
	if b.ExpectedRevision != op.MappingRevision {
		return ports.ApprovalView{}, domain.NewError(domain.CodeConflict, "recovery revision changed")
	}
	if e = domain.ValidateRoleMappings(b.Mappings); e != nil {
		return ports.ApprovalView{}, e
	}
	service := PolicyAdministration{Store: s.Store, ApprovalProvider: s.Provider, Clock: s.Clock}
	return service.RequestLocalApproval(ctx, op, domain.ApprovalEmergencyExpansion, "role_mappings", "role-mappings", domain.RoleMappingDigest(op.TenantID, s.Issuer, b.ExpectedRevision, b.Mappings), "role-mappings.recover", 10*time.Minute)
}
func (s *OperatorRecovery) Approve(ctx context.Context, p identity.Principal, key string, id domain.ID) (ports.ApprovalView, error) {
	op, e := s.operation(ctx, p, key, "operator.role_mappings.recovery.approve", id)
	if e != nil {
		return ports.ApprovalView{}, e
	}
	a, e := s.Store.GovernanceApproval(ctx, id)
	if e != nil {
		return ports.ApprovalView{}, e
	}
	if a.Action != domain.ApprovalEmergencyExpansion || a.ReasonCode != "role-mappings.recover" {
		return ports.ApprovalView{}, domain.NewError(domain.CodeForbidden, "not a recovery approval")
	}
	return s.Store.DecideLocalApproval(ctx, op, id, true)
}
func (s *OperatorRecovery) Apply(ctx context.Context, p identity.Principal, key string, b UpdateRoleMappings) (ports.RoleMappings, error) {
	op, e := s.operation(ctx, p, key, "operator.role_mappings.recovery.apply", b)
	if e != nil {
		return ports.RoleMappings{}, e
	}
	if b.ExpectedRevision < 1 || b.ExpectedRevision == 1<<63-1 {
		return ports.RoleMappings{}, domain.NewError(domain.CodeInvalidArgument, "invalid expected revision")
	}
	return s.Store.SaveRoleMappings(ctx, op, ports.RoleMappings{Issuer: s.Issuer, Revision: b.ExpectedRevision, Mappings: b.Mappings}, b.ApprovalReference)
}
