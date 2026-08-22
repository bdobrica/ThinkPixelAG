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

type RunCancellationService struct {
	repository ports.RunCancellationRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type CancelRun struct {
	TenantID, PrincipalID, RequestID, RunID domain.ID
	Roles                                   []string
	Issuer, IdempotencyKey, ReasonCode      string
	ExpectedStateVersion                    *int64
	SecurityState                           policy.SecurityState
}

func NewRunCancellationService(repository ports.RunCancellationRepository, evaluator policy.Evaluator, clock domain.Clock) (*RunCancellationService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("run cancellation requires a repository, policy evaluator, and clock")
	}
	return &RunCancellationService{repository: repository, evaluator: evaluator, clock: clock}, nil
}

func (service *RunCancellationService) Cancel(ctx context.Context, command CancelRun) (domain.Run, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.RunID.IsZero() || command.IdempotencyKey == "" {
		return domain.Run{}, domain.NewError(domain.CodeInvalidArgument, "run cancellation is invalid")
	}
	record, err := service.repository.GetRun(ctx, command.RunID)
	if err != nil {
		return domain.Run{}, runNotFound(err)
	}
	if record.Run.TenantID != command.TenantID || record.AgentOwnerID.IsZero() || !record.AgentRiskClass.Valid() {
		return domain.Run{}, domain.NewError(domain.CodeInternal, "run cancellation repository crossed its authority boundary")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "run cancellation clock is invalid", err)
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "could not generate run cancellation decision identifier", err)
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles, Issuer: command.Issuer},
		Action:  "runs.cancel", Resource: policy.Resource{Type: "run", ID: command.RunID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"state": string(record.Run.State), "state_version": record.Run.StateVersion, "requested_by": record.Run.RequestedBy.String()}},
		Agent: &policy.Agent{ID: record.Run.AgentID.String(), VersionDigest: record.Run.VersionDigest, RiskClass: strings.ToLower(string(record.AgentRiskClass)), Owner: record.AgentOwnerID.String(), Approved: true}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}}
	result, err := service.evaluator.Decide(ctx, input)
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeUnavailable, "run cancellation policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.Run{}, domain.NewError(domain.CodeUnavailable, "run cancellation policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.Run{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	ids := make([]domain.ID, 4)
	for i := range ids {
		ids[i], err = domain.NewID()
		if err != nil {
			return domain.Run{}, domain.WrapError(domain.CodeInternal, "could not generate run cancellation evidence identifier", err)
		}
	}
	cancellation := domain.RunCancellation{TenantID: command.TenantID, RunID: command.RunID, ActorPrincipalID: command.PrincipalID, IdempotencyKey: "cancel:" + command.IdempotencyKey, ReasonCode: command.ReasonCode, ExpectedStateVersion: command.ExpectedStateVersion, CreatedAt: now}
	if err := cancellation.Validate(); err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInvalidArgument, "run cancellation is invalid", err)
	}
	evidence := ports.RunCancellationEvidence{SignalID: ids[0], EventID: ids[1], AuditID: ids[2], OutboxID: ids[3], RequestID: command.RequestID, PolicyDecisionID: decisionID, ReasonCodes: result.Decision.ReasonCodes}
	return service.repository.CancelRun(ctx, cancellation, evidence)
}
