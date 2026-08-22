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

type RunSignalService struct {
	repository ports.RunSignalRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type SignalRun struct {
	TenantID, PrincipalID, RequestID, RunID domain.ID
	Roles                                   []string
	Issuer, IdempotencyKey                  string
	Type                                    domain.RunSignalType
	Payload                                 []byte
	ExpectedStateVersion                    *int64
	SecurityState                           policy.SecurityState
}

func NewRunSignalService(repository ports.RunSignalRepository, evaluator policy.Evaluator, clock domain.Clock) (*RunSignalService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("run signal requires a repository, policy evaluator, and clock")
	}
	return &RunSignalService{repository: repository, evaluator: evaluator, clock: clock}, nil
}

func (service *RunSignalService) Signal(ctx context.Context, command SignalRun) (domain.RunEvent, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.RunID.IsZero() || command.IdempotencyKey == "" {
		return domain.RunEvent{}, domain.NewError(domain.CodeInvalidArgument, "run signal is invalid")
	}
	record, err := service.repository.GetRun(ctx, command.RunID)
	if err != nil {
		return domain.RunEvent{}, runNotFound(err)
	}
	if record.Run.TenantID != command.TenantID || record.AgentOwnerID.IsZero() || !record.AgentRiskClass.Valid() {
		return domain.RunEvent{}, domain.NewError(domain.CodeInternal, "run signal repository crossed its authority boundary")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunEvent{}, domain.WrapError(domain.CodeInternal, "run signal clock is invalid", err)
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.RunEvent{}, domain.WrapError(domain.CodeInternal, "could not generate run signal decision identifier", err)
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles, Issuer: command.Issuer},
		Action:  "runs.signal", Resource: policy.Resource{Type: "run", ID: command.RunID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"state": string(record.Run.State), "state_version": record.Run.StateVersion, "signal_type": string(command.Type)}},
		Agent: &policy.Agent{ID: record.Run.AgentID.String(), VersionDigest: record.Run.VersionDigest, RiskClass: strings.ToLower(string(record.AgentRiskClass)), Owner: record.AgentOwnerID.String(), Approved: true}, RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}}
	result, err := service.evaluator.Decide(ctx, input)
	if err != nil {
		return domain.RunEvent{}, domain.WrapError(domain.CodeUnavailable, "run signal policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.RunEvent{}, domain.NewError(domain.CodeUnavailable, "run signal policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.RunEvent{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	ids := make([]domain.ID, 4)
	for i := range ids {
		ids[i], err = domain.NewID()
		if err != nil {
			return domain.RunEvent{}, domain.WrapError(domain.CodeInternal, "could not generate run signal evidence identifier", err)
		}
	}
	signal := domain.RunSignal{ID: ids[0], TenantID: command.TenantID, RunID: command.RunID, ActorPrincipalID: command.PrincipalID, Type: command.Type, Payload: append([]byte(nil), command.Payload...), IdempotencyKey: command.IdempotencyKey, ExpectedStateVersion: command.ExpectedStateVersion, CreatedAt: now}
	if err := signal.Validate(); err != nil {
		return domain.RunEvent{}, domain.WrapError(domain.CodeInvalidArgument, "run signal payload is invalid", err)
	}
	evidence := ports.RunSignalEvidence{EventID: ids[1], AuditID: ids[2], OutboxID: ids[3], RequestID: command.RequestID, PolicyDecisionID: decisionID, ReasonCodes: result.Decision.ReasonCodes}
	return service.repository.AppendRunSignal(ctx, signal, evidence)
}
