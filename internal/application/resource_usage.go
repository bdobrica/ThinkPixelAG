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

type TrustedUsageService struct {
	repository ports.TrustedUsageRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

type RecordTrustedUsage struct {
	TenantID, ProducerID, RequestID, RunID    domain.ID
	Roles                                     []string
	Issuer, SourceEventID, ResourceName, Unit string
	Quantity                                  int64
	ObservedAt                                time.Time
	SecurityState                             policy.SecurityState
}

func NewTrustedUsageService(repository ports.TrustedUsageRepository, evaluator policy.Evaluator, clock domain.Clock) (*TrustedUsageService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("trusted usage requires a repository, policy evaluator, and clock")
	}
	return &TrustedUsageService{repository, evaluator, clock}, nil
}

func (s *TrustedUsageService) Record(ctx context.Context, command RecordTrustedUsage) (domain.UsageReceipt, error) {
	if command.TenantID.IsZero() || command.ProducerID.IsZero() || command.RequestID.IsZero() || command.RunID.IsZero() || command.Quantity < 0 {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInvalidArgument, "trusted usage is invalid")
	}
	record, err := s.repository.GetRun(ctx, command.RunID)
	if err != nil {
		return domain.UsageReceipt{}, runNotFound(err)
	}
	if record.Run.TenantID != command.TenantID {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInternal, "trusted usage repository crossed its authority boundary")
	}
	now, err := domain.RequireUTC(s.clock.Now())
	if err != nil {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInternal, "trusted usage clock is invalid")
	}
	decisionID, err := domain.NewID()
	if err != nil {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInternal, "could not generate metering decision identifier")
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	result, err := s.evaluator.Decide(ctx, policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.ProducerID.String(), TenantID: command.TenantID.String(), PrincipalType: "workload", Roles: roles, Issuer: command.Issuer},
		Action:  "resources.meter", Resource: policy.Resource{Type: "run", ID: command.RunID.String(), TenantID: command.TenantID.String(), Attributes: map[string]any{"resource_name": command.ResourceName, "unit": command.Unit}},
		Agent:                &policy.Agent{ID: record.Run.AgentID.String(), VersionDigest: record.Run.VersionDigest, RiskClass: strings.ToLower(string(record.AgentRiskClass)), Owner: record.AgentOwnerID.String(), Approved: true},
		RequestedConstraints: map[string]any{}, AuthorityConstraints: map[string]any{}, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}})
	if err != nil {
		return domain.UsageReceipt{}, domain.WrapError(domain.CodeUnavailable, "metering policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeUnavailable, "metering policy returned invalid evidence").WithRetryable()
	}
	if !result.Decision.Allow {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeForbidden, "producer is not authorized to meter this resource")
	}
	usageID, err := domain.NewID()
	if err != nil {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInternal, "could not generate usage identifier")
	}
	decimal, err := domain.NewDecimal(command.Quantity, 0)
	if err != nil {
		return domain.UsageReceipt{}, domain.NewError(domain.CodeInvalidArgument, "trusted usage quantity is invalid")
	}
	quantity, err := domain.NewQuantity(decimal, command.Unit)
	if err != nil {
		return domain.UsageReceipt{}, domain.WrapError(domain.CodeInvalidArgument, "trusted usage quantity is invalid", err)
	}
	usage := domain.TrustedUsage{ID: usageID, TenantID: command.TenantID, RunID: command.RunID, ProducerID: command.ProducerID, SourceEventID: command.SourceEventID, ResourceName: command.ResourceName, Quantity: quantity, ObservedAt: command.ObservedAt, RecordedAt: now}
	return s.repository.RecordTrustedUsage(ctx, usage)
}
