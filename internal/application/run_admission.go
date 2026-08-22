package application

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maximumAdmissionJSONBytes = 64 << 10

type RunAdmissionService struct {
	resolver   *VersionResolver
	repository ports.RunAdmissionRepository
	clock      domain.Clock
}

func NewRunAdmissionService(resolver *VersionResolver, repository ports.RunAdmissionRepository, clock domain.Clock) (*RunAdmissionService, error) {
	if resolver == nil || repository == nil || clock == nil {
		return nil, errors.New("run admission requires a resolver, repository, and clock")
	}
	return &RunAdmissionService{resolver: resolver, repository: repository, clock: clock}, nil
}

// AdmitRun contains only identity derived from authentication and bounded
// governance inputs. Objective and agent input remain transport/runtime data.
type AdmitRun struct {
	TenantID, PrincipalID, AgentID, RequestID  domain.ID
	Roles                                      []string
	RequestedVersionDigest                     string
	RequestedConstraints, AuthorityConstraints map[string]any
	SecurityState                              policy.SecurityState
}

func (service *RunAdmissionService) Admit(ctx context.Context, command AdmitRun) (domain.RunAdmission, error) {
	if command.TenantID.IsZero() || command.PrincipalID.IsZero() || command.AgentID.IsZero() || command.RequestID.IsZero() || command.RequestedConstraints == nil || command.AuthorityConstraints == nil {
		return domain.RunAdmission{}, domain.NewError(domain.CodeUnauthenticated, "authenticated run admission identity is required")
	}
	if err := boundedJSONObject(command.RequestedConstraints); err != nil {
		return domain.RunAdmission{}, err
	}
	if err := boundedJSONObject(command.AuthorityConstraints); err != nil {
		return domain.RunAdmission{}, err
	}
	runID, err := domain.NewID()
	if err != nil {
		return domain.RunAdmission{}, domain.WrapError(domain.CodeInternal, "could not generate run identifier", err)
	}
	resolution, err := service.resolver.Resolve(ctx, ResolveAgentVersion{RunID: runID, TenantID: command.TenantID, AgentID: command.AgentID, PrincipalID: command.PrincipalID, RequestID: command.RequestID, Roles: command.Roles, RequestedVersionDigest: command.RequestedVersionDigest, RequestedConstraints: command.RequestedConstraints, AuthorityConstraints: command.AuthorityConstraints, SecurityState: command.SecurityState})
	if err != nil {
		return domain.RunAdmission{}, err
	}
	if err := boundedJSONObject(resolution.ResolvedConstraints); err != nil {
		return domain.RunAdmission{}, domain.WrapError(domain.CodeUnavailable, "policy resolved constraints are invalid", err).WithRetryable()
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.RunAdmission{}, domain.WrapError(domain.CodeInternal, "run admission clock is invalid", err)
	}
	deadline, err := admissionDeadline(now, resolution.ResolvedConstraints)
	if err != nil {
		return domain.RunAdmission{}, err
	}
	ids := make([]domain.ID, 4)
	for index := range ids {
		ids[index], err = domain.NewID()
		if err != nil {
			return domain.RunAdmission{}, domain.WrapError(domain.CodeInternal, "could not generate admission evidence identifier", err)
		}
	}
	admission := domain.RunAdmission{RunID: runID, EnvelopeID: ids[0], TenantID: command.TenantID, AgentID: command.AgentID, AgentVersionID: resolution.AgentVersionID, AgentVersionDigest: resolution.AgentContentDigest, RequestedBy: command.PrincipalID, PolicyDecisionID: resolution.InvocationDecisionID, State: domain.RunAdmitted, StateVersion: 1, Constraints: resolution.ResolvedConstraints, DeadlineAt: deadline, CreatedAt: now, UpdatedAt: now}
	if err := admission.Validate(); err != nil {
		return domain.RunAdmission{}, domain.WrapError(domain.CodeInternal, "constructed run admission is invalid", err)
	}
	evidence := ports.RunAdmissionEvidence{EventID: ids[1], AuditID: ids[2], OutboxID: ids[3], RequestID: command.RequestID, ReasonCodes: []string{"agent.invoke.allowed"}}
	if err := service.repository.AdmitRun(ctx, admission, resolution, evidence); err != nil {
		return domain.RunAdmission{}, err
	}
	return admission, nil
}

func boundedJSONObject(value map[string]any) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maximumAdmissionJSONBytes {
		return domain.NewError(domain.CodeInvalidArgument, "run constraints are invalid or exceed bounds")
	}
	return nil
}

func admissionDeadline(now time.Time, constraints map[string]any) (*time.Time, error) {
	value, exists := constraints["max_execution_time_seconds"]
	if !exists {
		return nil, nil
	}
	var seconds int64
	switch typed := value.(type) {
	case float64:
		if typed != math.Trunc(typed) || typed < 1 || typed > 604800 {
			return nil, domain.NewError(domain.CodeUnavailable, "policy resolved an invalid execution deadline").WithRetryable()
		}
		seconds = int64(typed)
	case int64:
		seconds = typed
	case int:
		seconds = int64(typed)
	case json.Number:
		var err error
		seconds, err = typed.Int64()
		if err != nil {
			return nil, domain.NewError(domain.CodeUnavailable, "policy resolved an invalid execution deadline").WithRetryable()
		}
	default:
		return nil, domain.NewError(domain.CodeUnavailable, "policy resolved an invalid execution deadline").WithRetryable()
	}
	if seconds < 1 || seconds > 604800 {
		return nil, domain.NewError(domain.CodeUnavailable, "policy resolved an invalid execution deadline").WithRetryable()
	}
	deadline := now.Add(time.Duration(seconds) * time.Second)
	return &deadline, nil
}
