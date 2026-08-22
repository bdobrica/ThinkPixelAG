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

type VersionResolver struct {
	repository ports.VersionResolutionRepository
	evaluator  policy.Evaluator
	clock      domain.Clock
}

func NewVersionResolver(repository ports.VersionResolutionRepository, evaluator policy.Evaluator, clock domain.Clock) (*VersionResolver, error) {
	if repository == nil || evaluator == nil || clock == nil {
		return nil, errors.New("version resolver requires a repository, policy evaluator, and clock")
	}
	return &VersionResolver{repository: repository, evaluator: evaluator, clock: clock}, nil
}

type ResolveAgentVersion struct {
	RunID, TenantID, AgentID, PrincipalID, RequestID domain.ID
	Roles                                            []string
	RequestedVersionDigest                           string
	RequestedConstraints, AuthorityConstraints       map[string]any
	SecurityState                                    policy.SecurityState
}

func (resolver *VersionResolver) Resolve(ctx context.Context, command ResolveAgentVersion) (domain.RunVersionResolution, error) {
	if command.RunID.IsZero() || command.TenantID.IsZero() || command.AgentID.IsZero() || command.PrincipalID.IsZero() || command.RequestID.IsZero() || command.RequestedConstraints == nil || command.AuthorityConstraints == nil {
		return domain.RunVersionResolution{}, domain.NewError(domain.CodeInvalidArgument, "version resolution request is invalid")
	}
	if command.RequestedVersionDigest != "" && !domain.ValidDigest(command.RequestedVersionDigest) {
		return domain.RunVersionResolution{}, domain.NewError(domain.CodeInvalidArgument, "requested version digest is invalid")
	}
	candidates, err := resolver.repository.ListAgentVersionCandidates(ctx, command.AgentID, command.RequestedVersionDigest)
	if err != nil {
		return domain.RunVersionResolution{}, err
	}
	if len(candidates) == 0 {
		return domain.RunVersionResolution{}, domain.NewError(domain.CodeNotFound, "no eligible agent version found")
	}
	now, err := domain.RequireUTC(resolver.clock.Now())
	if err != nil {
		return domain.RunVersionResolution{}, domain.WrapError(domain.CodeInternal, "version resolver clock is invalid", err)
	}
	roles := append([]string(nil), command.Roles...)
	sort.Strings(roles)
	for _, candidate := range candidates {
		if candidate.Agent.TenantID != command.TenantID || candidate.Agent.ID != command.AgentID || candidate.Version.TenantID != command.TenantID || candidate.Version.AgentID != command.AgentID || candidate.Approval.AgentVersionID != candidate.Version.ID || candidate.Approval.Decision != domain.DecisionApprove {
			return domain.RunVersionResolution{}, domain.NewError(domain.CodeInternal, "version candidate crossed its authority boundary")
		}
		if command.RequestedVersionDigest == "" && candidate.State != domain.AgentVersionApproved || command.RequestedVersionDigest != "" && (len(candidates) != 1 || candidate.Version.ContentDigest != command.RequestedVersionDigest || candidate.State != domain.AgentVersionApproved && candidate.State != domain.AgentVersionDeprecated) {
			return domain.RunVersionResolution{}, domain.NewError(domain.CodeInternal, "version repository returned an ineligible candidate")
		}
		mode := domain.ResolutionAutomatic
		var selection policy.Result
		if command.RequestedVersionDigest != "" {
			mode, selection, err = resolver.authorizeSelection(ctx, command, candidate, roles, now)
			if err != nil {
				return domain.RunVersionResolution{}, err
			}
		}
		invocation, decisionErr := resolver.decide(ctx, command, candidate, roles, "runs.create", now)
		if decisionErr != nil {
			return domain.RunVersionResolution{}, decisionErr
		}
		if !invocation.Decision.Allow {
			if command.RequestedVersionDigest != "" {
				return domain.RunVersionResolution{}, domain.NewError(domain.CodeForbidden, "agent version invocation is not permitted")
			}
			continue
		}
		if mode != domain.ResolutionAutomatic && (selection.Metadata.PolicyDigest != invocation.Metadata.PolicyDigest || selection.Metadata.PolicyVersion != invocation.Metadata.PolicyVersion) {
			return domain.RunVersionResolution{}, domain.NewError(domain.CodeUnavailable, "policy changed during controlled version selection").WithRetryable()
		}
		invocationID, parseErr := domain.ParseID(invocation.Decision.DecisionID)
		if parseErr != nil {
			return domain.RunVersionResolution{}, domain.NewError(domain.CodeUnavailable, "policy returned invalid resolution evidence").WithRetryable()
		}
		resolution := domain.RunVersionResolution{RunID: command.RunID, TenantID: command.TenantID, AgentID: command.AgentID, AgentVersionID: candidate.Version.ID, ApprovalID: candidate.Approval.ID,
			AgentContentDigest: candidate.Version.ContentDigest, PolicyBundleDigest: invocation.Metadata.PolicyDigest, PolicyActivationVersion: invocation.Metadata.PolicyVersion,
			Mode: mode, InvocationDecisionID: invocationID, ResolvedConstraints: invocation.Decision.ResolvedConstraints, ResolvedAt: now}
		if mode != domain.ResolutionAutomatic {
			resolution.SelectionDecisionID, parseErr = domain.ParseID(selection.Decision.DecisionID)
			if parseErr != nil {
				return domain.RunVersionResolution{}, domain.NewError(domain.CodeUnavailable, "policy returned invalid selection evidence").WithRetryable()
			}
		}
		if err := resolution.Validate(); err != nil {
			return domain.RunVersionResolution{}, domain.WrapError(domain.CodeInternal, "resolved version evidence is invalid", err)
		}
		return resolution, nil
	}
	return domain.RunVersionResolution{}, domain.NewError(domain.CodeForbidden, "no eligible agent version is permitted")
}

func (resolver *VersionResolver) authorizeSelection(ctx context.Context, command ResolveAgentVersion, candidate domain.AgentVersionCandidate, roles []string, now time.Time) (domain.VersionResolutionMode, policy.Result, error) {
	mode, action := domain.ResolutionPinned, "versions.pin"
	if candidate.State == domain.AgentVersionDeprecated {
		mode, action = domain.ResolutionRollback, "versions.rollback"
	}
	result, err := resolver.decide(ctx, command, candidate, roles, action, now)
	if err != nil {
		return "", policy.Result{}, err
	}
	if !result.Decision.Allow {
		return "", policy.Result{}, domain.NewError(domain.CodeForbidden, "requested version selection is not permitted")
	}
	return mode, result, nil
}

func (resolver *VersionResolver) Persist(ctx context.Context, resolution domain.RunVersionResolution) error {
	if err := resolution.Validate(); err != nil {
		return domain.WrapError(domain.CodeInvalidArgument, "version resolution snapshot is invalid", err)
	}
	return resolver.repository.PersistRunVersionResolution(ctx, resolution)
}

func (resolver *VersionResolver) decide(ctx context.Context, command ResolveAgentVersion, candidate domain.AgentVersionCandidate, roles []string, action string, now time.Time) (policy.Result, error) {
	decisionID, err := domain.NewID()
	if err != nil {
		return policy.Result{}, domain.WrapError(domain.CodeInternal, "could not generate policy decision identifier", err)
	}
	input := policy.Input{ContractVersion: policy.ContractVersion, DecisionID: decisionID.String(), RequestTime: now,
		Subject: policy.Subject{PrincipalID: command.PrincipalID.String(), TenantID: command.TenantID.String(), PrincipalType: "human", Roles: roles}, Action: action,
		Resource:             policy.Resource{Type: "agent_version", ID: candidate.Version.ContentDigest, TenantID: command.TenantID.String(), Attributes: map[string]any{"selection_state": strings.ToLower(string(candidate.State))}},
		Agent:                &policy.Agent{ID: candidate.Agent.ID.String(), VersionDigest: candidate.Version.ContentDigest, RiskClass: strings.ToLower(string(candidate.Agent.RiskClass)), Owner: candidate.Agent.OwnerPrincipalID.String(), Approved: true},
		RequestedConstraints: command.RequestedConstraints, AuthorityConstraints: command.AuthorityConstraints, SecurityState: command.SecurityState, Context: policy.RequestContext{RequestID: command.RequestID.String()}}
	result, err := resolver.evaluator.Decide(ctx, input)
	if err != nil {
		return policy.Result{}, domain.WrapError(domain.CodeUnavailable, "version resolution policy is unavailable", err).WithRetryable()
	}
	if result.Decision.DecisionID != decisionID.String() || !domain.ValidDigest(result.Metadata.PolicyDigest) || result.Metadata.PolicyVersion < 1 {
		return policy.Result{}, domain.NewError(domain.CodeUnavailable, "version resolution policy returned invalid evidence").WithRetryable()
	}
	return result, nil
}
