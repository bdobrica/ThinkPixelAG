package application

import (
	"context"
	"errors"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type AgentApprovalAuthorizer interface {
	AuthorizeAgentVersionDecision(context.Context, AgentVersionDecisionAuthorization) (domain.ID, error)
}

type AgentVersionDecisionAuthorization struct {
	TenantID, ActorPrincipalID, AgentID domain.ID
	VersionDigest                       string
	Decision                            domain.AgentVersionDecision
	Roles                               []string
	RequestID                           domain.ID
}

type AgentApprovalRegistry struct {
	repository ports.AgentApprovalRegistry
	authorizer AgentApprovalAuthorizer
	clock      domain.Clock
}

func NewAgentApprovalRegistry(repository ports.AgentApprovalRegistry, authorizer AgentApprovalAuthorizer, clock domain.Clock) (*AgentApprovalRegistry, error) {
	if repository == nil || authorizer == nil || clock == nil {
		return nil, errors.New("agent approval registry requires a repository, authorizer, and clock")
	}
	return &AgentApprovalRegistry{repository: repository, authorizer: authorizer, clock: clock}, nil
}

type DecideAgentVersion struct {
	ID, TenantID, AgentID, ActorPrincipalID, RequestID domain.ID
	VersionDigest                                      string
	Decision                                           domain.AgentVersionDecision
	ReasonCode, ApprovalReference                      string
	Roles                                              []string
}

func (service *AgentApprovalRegistry) Decide(ctx context.Context, command DecideAgentVersion) (domain.AgentVersionApproval, error) {
	if command.ID.IsZero() || command.TenantID.IsZero() || command.AgentID.IsZero() || command.ActorPrincipalID.IsZero() || command.RequestID.IsZero() || len(command.VersionDigest) != 71 || !command.Decision.Valid() {
		return domain.AgentVersionApproval{}, domain.NewError(domain.CodeInvalidArgument, "agent version decision is invalid")
	}
	policyDecisionID, err := service.authorizer.AuthorizeAgentVersionDecision(ctx, AgentVersionDecisionAuthorization{
		TenantID: command.TenantID, ActorPrincipalID: command.ActorPrincipalID, AgentID: command.AgentID,
		VersionDigest: command.VersionDigest, Decision: command.Decision, Roles: append([]string(nil), command.Roles...), RequestID: command.RequestID,
	})
	if err != nil {
		return domain.AgentVersionApproval{}, err
	}
	if policyDecisionID.IsZero() {
		return domain.AgentVersionApproval{}, domain.NewError(domain.CodeForbidden, "agent version decision was not authorized")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeInternal, "agent approval registry clock is invalid", err)
	}
	approval := domain.AgentVersionApproval{ID: command.ID, TenantID: command.TenantID, AgentID: command.AgentID, ActorPrincipalID: command.ActorPrincipalID,
		PolicyDecisionID: policyDecisionID, Decision: command.Decision, ReasonCode: command.ReasonCode, ApprovalReference: command.ApprovalReference, CreatedAt: now}
	// The repository resolves the immutable version ID from the tenant-scoped
	// digest. Use a non-zero placeholder solely to validate every other field
	// before any database mutation can occur.
	approval.AgentVersionID = approval.ID
	if err := approval.Validate(); err != nil {
		return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeInvalidArgument, "agent version decision is invalid", err)
	}
	approval.AgentVersionID = domain.ID{}
	auditID, err := domain.NewID()
	if err != nil {
		return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeInternal, "could not generate audit identifier", err)
	}
	outboxID, err := domain.NewID()
	if err != nil {
		return domain.AgentVersionApproval{}, domain.WrapError(domain.CodeInternal, "could not generate outbox identifier", err)
	}
	approval, err = service.repository.RecordAgentVersionDecision(ctx, approval, command.VersionDigest, auditID, outboxID, &command.RequestID)
	if err != nil {
		return domain.AgentVersionApproval{}, err
	}
	return approval, nil
}

func (service *AgentApprovalRegistry) Eligibility(ctx context.Context, tenantID, agentID domain.ID, digest string) (domain.AgentVersionState, error) {
	if tenantID.IsZero() || agentID.IsZero() || len(digest) != 71 {
		return "", domain.NewError(domain.CodeInvalidArgument, "tenant, agent, and version digest are required")
	}
	return service.repository.AgentVersionEligibility(ctx, agentID, digest)
}
