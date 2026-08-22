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

type RunQuery struct {
	repository ports.RunQueryRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type GetRun struct {
	TenantID, PrincipalID, RequestID, RunID domain.ID
	Roles                                   []string
	Issuer                                  string
	SecurityState                           policy.SecurityState
}

func NewRunQuery(repository ports.RunQueryRepository, evaluator policy.Evaluator, clock domain.Clock) (*RunQuery, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("run query requires a repository, policy evaluator, and clock")
	}
	return &RunQuery{repository: repository, evaluator: evaluator, clock: clock}, nil
}

func (service *RunQuery) Get(ctx context.Context, command GetRun) (domain.Run, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.RunID.IsZero() {
		return domain.Run{}, domain.NewError(domain.CodeInvalidArgument, "run query is invalid")
	}
	record, err := service.repository.GetRun(ctx, command.RunID)
	if err != nil {
		return domain.Run{}, runNotFound(err)
	}
	if record.Run.TenantID != command.TenantID || record.AgentOwnerID.IsZero() || !record.AgentRiskClass.Valid() {
		return domain.Run{}, domain.NewError(domain.CodeInternal, "run query repository crossed its authority boundary")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "run query clock is invalid", err)
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeInternal, "could not generate run access decision identifier", err)
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles, Issuer: command.Issuer},
		Action:  "runs.read", Resource: policy.Resource{Type: "run", ID: record.Run.ID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"state": string(record.Run.State), "requested_by": record.Run.RequestedBy.String()}},
		Agent:                &policy.Agent{ID: record.Run.AgentID.String(), VersionDigest: record.Run.VersionDigest, RiskClass: strings.ToLower(string(record.AgentRiskClass)), Owner: record.AgentOwnerID.String(), Approved: true},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}}
	result, err := service.evaluator.Decide(ctx, input)
	if err != nil {
		return domain.Run{}, domain.WrapError(domain.CodeUnavailable, "run access policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.Run{}, domain.NewError(domain.CodeUnavailable, "run access policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.Run{}, domain.NewError(domain.CodeNotFound, "run not found")
	}
	return record.Run, nil
}

func runNotFound(err error) error {
	var typed *domain.Error
	if errors.As(err, &typed) && typed.Code() == domain.CodeNotFound {
		return domain.NewError(domain.CodeNotFound, "run not found")
	}
	return err
}
