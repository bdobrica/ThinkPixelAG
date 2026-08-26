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

type ResourceExtensionService struct {
	repository ports.ResourceExtensionRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type ExtendResources struct {
	TenantID, PrincipalID, RequestID, RunID               domain.ID
	Roles                                                 []string
	Issuer, IdempotencyKey, ReasonCode, ApprovalReference string
	Additions                                             []domain.ResourceExtensionAmount
	DeadlineExtensionSeconds                              int64
	SecurityState                                         policy.SecurityState
}

func NewResourceExtensionService(repository ports.ResourceExtensionRepository, evaluator policy.Evaluator, clock domain.Clock) (*ResourceExtensionService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("resource extension requires a repository, policy evaluator, and clock")
	}
	return &ResourceExtensionService{repository: repository, evaluator: evaluator, clock: clock}, nil
}

func (s *ResourceExtensionService) Extend(ctx context.Context, command ExtendResources) (domain.ResourceExtensionResult, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.RunID.IsZero() {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeInvalidArgument, "resource extension is invalid")
	}
	record, err := s.repository.GetRun(ctx, command.RunID)
	if err != nil {
		return domain.ResourceExtensionResult{}, runNotFound(err)
	}
	if record.Run.TenantID != command.TenantID || record.AgentOwnerID.IsZero() || !record.AgentRiskClass.Valid() {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeInternal, "resource extension repository crossed its authority boundary")
	}
	// The run's requesting identity is never allowed to grant itself more authority,
	// even if a policy bundle is accidentally permissive.
	if command.PrincipalID == record.Run.RequestedBy {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeForbidden, "a run requester cannot extend its own resources")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeInternal, "resource extension clock is invalid")
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeInternal, "could not generate extension decision identifier")
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	requested := map[string]any{"deadline_extension_seconds": command.DeadlineExtensionSeconds, "reason_code": command.ReasonCode, "approval_reference": command.ApprovalReference}
	additions := make([]any, 0, len(command.Additions))
	for _, a := range command.Additions {
		additions = append(additions, map[string]any{"name": a.Name, "unit": a.Quantity.Unit(), "quantity": a.Quantity.Amount().Coefficient()})
	}
	requested["additions"] = additions
	result, err := s.evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now, Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles, Issuer: command.Issuer}, Action: "resources.extend", Resource: policy.Resource{Type: "envelope", ID: command.RunID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"state": string(record.Run.State), "requested_by": record.Run.RequestedBy.String(), "envelope_version": record.Run.EnvelopeVersion}}, Agent: &policy.Agent{ID: record.Run.AgentID.String(), VersionDigest: record.Run.VersionDigest, RiskClass: strings.ToLower(string(record.AgentRiskClass)), Owner: record.AgentOwnerID.String(), Approved: true}, RequestedConstraints: requested, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}})
	if err != nil {
		return domain.ResourceExtensionResult{}, domain.WrapError(domain.CodeUnavailable, "resource extension policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeUnavailable, "resource extension policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.ResourceExtensionResult{}, domain.NewError(domain.CodeForbidden, "resource extension is not authorized")
	}
	ids := make([]domain.ID, 4)
	for index := range ids {
		ids[index], err = domain.NewID()
		if err != nil {
			return domain.ResourceExtensionResult{}, domain.WrapError(domain.CodeInternal, "could not generate resource extension evidence identifier", err)
		}
	}
	extensionID, auditID, outboxID, eventID := ids[0], ids[1], ids[2], ids[3]
	extension, err := domain.ValidateResourceExtension(domain.ResourceExtension{ID: extensionID, TenantID: command.TenantID, RunID: command.RunID, ActorPrincipalID: command.PrincipalID, PolicyDecisionID: decisionID, IdempotencyKey: command.IdempotencyKey, ReasonCode: command.ReasonCode, ApprovalReference: command.ApprovalReference, Additions: command.Additions, DeadlineExtensionSeconds: command.DeadlineExtensionSeconds, CreatedAt: now})
	if err != nil {
		return domain.ResourceExtensionResult{}, domain.WrapError(domain.CodeInvalidArgument, "resource extension is invalid", err)
	}
	return s.repository.ExtendResources(ctx, extension, ports.ResourceExtensionEvidence{AuditID: auditID, OutboxID: outboxID, EventID: eventID, RequestID: command.RequestID, ReasonCodes: result.Decision.ReasonCodes})
}
