package application

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type RevocationService struct {
	repository ports.RevocationRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
	invalidate func(string)
}
type ChangeRevocation struct {
	TenantID                                               domain.ID
	PrincipalID, RequestID, RevocationID                   domain.ID
	Roles                                                  []string
	Issuer                                                 string
	Scope                                                  domain.RevocationScope
	Target, ReasonCode, DetailReference, ApprovalReference string
	EffectiveAt                                            time.Time
	ExpiresAt                                              *time.Time
	SecurityState                                          policy.SecurityState
}

func NewRevocationService(r ports.RevocationRepository, e policy.Evaluator, c domain.Clock) (*RevocationService, error) {
	if r == nil || e == nil || c == nil {
		return nil, errors.New("revocation service requires repository, policy evaluator, and clock")
	}
	return &RevocationService{repository: r, evaluator: e, clock: c}, nil
}

// SetCacheInvalidator wires committed revocation changes to decision caches.
// An empty tenant denotes a global invalidation.
func (s *RevocationService) SetCacheInvalidator(invalidate func(string)) {
	s.invalidate = invalidate
}

func (s *RevocationService) Create(ctx context.Context, c ChangeRevocation) (domain.RevocationResult, error) {
	if c.TenantID.IsZero() || c.PrincipalID.IsZero() || c.RequestID.IsZero() {
		return domain.RevocationResult{}, domain.NewError(domain.CodeInvalidArgument, "revocation request is invalid")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.RevocationResult{}, domain.NewError(domain.CodeInternal, "revocation clock is invalid")
	}
	decision, evidence, err := s.authorize(ctx, c, "revocations.create", now)
	if err != nil {
		return domain.RevocationResult{}, err
	}
	id, err := domain.NewID()
	if err != nil {
		return domain.RevocationResult{}, err
	}
	var scopeTenant *domain.ID
	if c.Scope != domain.RevocationGlobal {
		scopeTenant = &c.TenantID
	}
	candidate := domain.Revocation{ID: id, TenantID: scopeTenant, ActorPrincipalID: c.PrincipalID, Scope: c.Scope, Target: c.Target, ReasonCode: c.ReasonCode, DetailReference: c.DetailReference, ApprovalReference: c.ApprovalReference, EffectiveAt: c.EffectiveAt, CreatedAt: now}
	if c.ExpiresAt != nil {
		x := time.Time(*c.ExpiresAt)
		candidate.ExpiresAt = &x
	}
	candidate, err = domain.ValidateRevocation(candidate)
	if err != nil {
		return domain.RevocationResult{}, domain.WrapError(domain.CodeInvalidArgument, "revocation is invalid", err)
	}
	evidence.PolicyDecisionID = decision
	result, err := s.repository.CreateRevocation(ctx, candidate, evidence)
	if err == nil {
		s.invalidateResult(result)
	}
	return result, err
}
func (s *RevocationService) Lift(ctx context.Context, c ChangeRevocation) (domain.RevocationResult, error) {
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.RevocationResult{}, err
	}
	decision, evidence, err := s.authorize(ctx, c, "revocations.lift", now)
	if err != nil {
		return domain.RevocationResult{}, err
	}
	lift, err := domain.ValidateRevocationLift(domain.RevocationLift{RevocationID: c.RevocationID, ActorPrincipalID: c.PrincipalID, ReasonCode: c.ReasonCode, ApprovalReference: c.ApprovalReference, ChangedAt: now})
	if err != nil {
		return domain.RevocationResult{}, domain.WrapError(domain.CodeInvalidArgument, "revocation lift is invalid", err)
	}
	evidence.PolicyDecisionID = decision
	result, err := s.repository.LiftRevocation(ctx, lift, evidence)
	if err == nil {
		s.invalidateResult(result)
	}
	return result, err
}

func (s *RevocationService) invalidateResult(result domain.RevocationResult) {
	if s.invalidate == nil {
		return
	}
	tenant := ""
	if result.Revocation.TenantID != nil {
		tenant = result.Revocation.TenantID.String()
	}
	s.invalidate(tenant)
}
func (s *RevocationService) authorize(ctx context.Context, c ChangeRevocation, action string, now time.Time) (domain.ID, ports.RevocationEvidence, error) {
	did, err := domain.NewID()
	if err != nil {
		return domain.ID{}, ports.RevocationEvidence{}, err
	}
	roles := append([]string(nil), c.Roles...)
	sort.Strings(roles)
	tenant := c.TenantID.String()
	result, err := s.evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: did.String(), RequestTime: now, Subject: policy.Subject{PrincipalID: c.PrincipalID.String(), TenantID: tenant, PrincipalType: "human", Roles: roles, Issuer: c.Issuer}, Action: action, Resource: policy.Resource{Type: "revocation", ID: c.RevocationID.String(), TenantID: tenant, Attributes: map[string]any{"scope": strings.ToLower(string(c.Scope)), "target": c.Target, "reason_code": c.ReasonCode, "approval_reference": c.ApprovalReference}}, SecurityState: c.SecurityState, Context: policy.RequestContext{RequestID: c.RequestID.String()}})
	if err != nil {
		return domain.ID{}, ports.RevocationEvidence{}, domain.WrapError(domain.CodeUnavailable, "revocation policy is unavailable", err).WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.ID{}, ports.RevocationEvidence{}, domain.NewError(domain.CodeForbidden, "revocation change is not authorized")
	}
	if result.Decision.DecisionID != did.String() {
		return domain.ID{}, ports.RevocationEvidence{}, domain.NewError(domain.CodeUnavailable, "revocation policy returned invalid evidence")
	}
	ids := make([]domain.ID, 4)
	for i := range ids {
		ids[i], err = domain.NewID()
		if err != nil {
			return domain.ID{}, ports.RevocationEvidence{}, err
		}
	}
	return did, ports.RevocationEvidence{ChangeID: ids[0], EventID: ids[1], AuditID: ids[2], OutboxID: ids[3], RequestID: c.RequestID, ReasonCodes: result.Decision.ReasonCodes}, nil
}
