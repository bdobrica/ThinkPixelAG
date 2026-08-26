package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type TrustedUsageService struct {
	repository  ports.TrustedUsageRepository
	evaluator   policy.Evaluator
	clock       domain.Clock
	accelerator ports.ThroughputAccelerator
}

type RecordTrustedUsage struct {
	TenantID, ProducerID, RequestID, RunID    domain.ID
	Roles                                     []string
	Issuer, SourceEventID, ResourceName, Unit string
	Quantity                                  int64
	ObservedAt                                time.Time
	SecurityState                             policy.SecurityState
}

func NewTrustedUsageService(repository ports.TrustedUsageRepository, evaluator policy.Evaluator, clock domain.Clock, accelerators ...ports.ThroughputAccelerator) (*TrustedUsageService, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("trusted usage requires a repository, policy evaluator, and clock")
	}
	if len(accelerators) > 1 {
		return nil, errors.New("trusted usage accepts at most one throughput accelerator")
	}
	var accelerator ports.ThroughputAccelerator
	if len(accelerators) == 1 {
		accelerator = accelerators[0]
	}
	return &TrustedUsageService{repository: repository, evaluator: evaluator, clock: clock, accelerator: accelerator}, nil
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
	rateDimension, _ := domain.ThroughputDimensionName(command.ResourceName)
	hint := domain.ThroughputHint{DimensionName: rateDimension}
	key := throughputKey(command.TenantID, command.RunID, rateDimension, now)
	if s.accelerator != nil && rateDimension != "" {
		// Cache failure is a safe bypass because PostgreSQL still performs the
		// complete authoritative check.
		hint.Blocked, _ = s.accelerator.Blocked(ctx, key)
	}
	disposition := domain.ExhaustionFail
	for _, obligation := range result.Decision.Obligations {
		if obligation.Type == "budget.pause_on_exhaustion" {
			disposition = domain.ExhaustionPause
		}
	}
	receipt, err := s.repository.RecordTrustedUsage(ctx, usage, hint, disposition)
	var exceeded *domain.ThroughputLimitExceeded
	if err != nil && errors.As(err, &exceeded) {
		if s.accelerator != nil {
			_ = s.accelerator.MarkBlocked(ctx, key, exceeded.RetryAt)
		}
		return domain.UsageReceipt{}, domain.NewError(domain.CodeConflict, "structural throughput limit exceeded").WithRetryable()
	}
	return receipt, err
}

func throughputKey(tenantID, runID domain.ID, dimension string, now time.Time) string {
	window := now.UTC().Truncate(domain.ThroughputWindow).Format(time.RFC3339)
	sum := sha256.Sum256([]byte(tenantID.String() + "\x00" + runID.String() + "\x00" + dimension + "\x00" + window))
	return "thinkpixelag:rate:v1:" + hex.EncodeToString(sum[:])
}
