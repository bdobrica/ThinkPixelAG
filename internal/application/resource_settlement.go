package application

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type ResourceSettlementService struct {
	repository ports.ResourceSettlementRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type SettleReservation struct {
	TenantID, PrincipalID, RequestID, ReservationID domain.ID
	Roles                                           []string
	Issuer, IdempotencyKey, TerminalRunState        string
	FinalUsageEventIDs                              []domain.ID
	SecurityState                                   policy.SecurityState
}

func NewResourceSettlementService(repository ports.ResourceSettlementRepository, evaluator policy.Evaluator, clock domain.Clock) (*ResourceSettlementService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("resource settlement requires a repository, policy evaluator, and clock")
	}
	return &ResourceSettlementService{repository: repository, evaluator: evaluator, clock: clock}, nil
}

func (s *ResourceSettlementService) Settle(ctx context.Context, command SettleReservation) (domain.ResourceSettlementResult, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.ReservationID.IsZero() {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeInvalidArgument, "resource settlement is invalid")
	}
	target, err := s.repository.GetSettlementTarget(ctx, command.ReservationID)
	if err != nil {
		return domain.ResourceSettlementResult{}, runNotFound(err)
	}
	if target.Run.Run.TenantID != command.TenantID {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeInternal, "settlement repository crossed its authority boundary")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeInternal, "settlement clock is invalid")
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeInternal, "could not generate settlement decision identifier")
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	decision, err := s.evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "workload", Roles: roles, Issuer: command.Issuer},
		Action:  "resources.settle", Resource: policy.Resource{Type: "reservation", ID: command.ReservationID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"child_run_id": target.ChildRunID.String(), "terminal_run_state": command.TerminalRunState}},
		Agent:                &policy.Agent{ID: target.Run.Run.AgentID.String(), VersionDigest: target.Run.Run.VersionDigest, RiskClass: strings.ToLower(string(target.Run.AgentRiskClass)), Owner: target.Run.AgentOwnerID.String(), Approved: true},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}})
	if err != nil {
		return domain.ResourceSettlementResult{}, domain.WrapError(domain.CodeUnavailable, "settlement policy is unavailable", err).WithRetryable()
	}
	if decision.Decision.DecisionID != decisionID.String() {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeUnavailable, "settlement policy returned invalid evidence").WithRetryable()
	}
	if !decision.Decision.Allow {
		return domain.ResourceSettlementResult{}, domain.NewError(domain.CodeForbidden, "workload is not authorized to settle this reservation")
	}
	settlementID, _ := domain.NewID()
	auditID, _ := domain.NewID()
	outboxID, _ := domain.NewID()
	settlement := domain.ResourceSettlement{ID: settlementID, TenantID: command.TenantID, ReservationID: command.ReservationID, ActorPrincipalID: command.PrincipalID, PolicyDecisionID: decisionID, IdempotencyKey: command.IdempotencyKey, TerminalRunState: command.TerminalRunState, FinalUsageEventIDs: command.FinalUsageEventIDs, SettledAt: now}
	settlement, err = domain.ValidateResourceSettlement(settlement)
	if err != nil {
		return domain.ResourceSettlementResult{}, domain.WrapError(domain.CodeInvalidArgument, "resource settlement is invalid", err)
	}
	return s.repository.SettleReservation(ctx, settlement, ports.ResourceSettlementEvidence{AuditID: auditID, OutboxID: outboxID, RequestID: command.RequestID, ReasonCodes: decision.Decision.ReasonCodes})
}
