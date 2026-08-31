package application

import (
	"context"
	"errors"
	"time"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

const maximumGovernanceApprovalLifetime = 24 * time.Hour

type GovernanceApprovalService struct {
	store        ports.GovernanceApprovalStore
	provider     ports.ApprovalProvider
	providerName string
	clock        domain.Clock
}

func NewGovernanceApprovalService(store ports.GovernanceApprovalStore, provider ports.ApprovalProvider, providerName string, clock domain.Clock) (*GovernanceApprovalService, error) {
	if store == nil || provider == nil || providerName == "" || len(providerName) > 128 || clock == nil {
		return nil, errors.New("governance approval service requires a store, named provider, and clock")
	}
	return &GovernanceApprovalService{store: store, provider: provider, providerName: providerName, clock: clock}, nil
}

type RequestGovernanceApproval struct {
	ID, TenantID, RequesterPrincipalID      domain.ID
	Action                                  domain.GovernanceApprovalAction
	ResourceType, ResourceID, RequestDigest string
	ReasonCode                              string
	ExpiresAt                               time.Time
}

func (service *GovernanceApprovalService) Request(ctx context.Context, command RequestGovernanceApproval) (domain.GovernanceApproval, error) {
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil || command.ExpiresAt.After(now.Add(maximumGovernanceApprovalLifetime)) {
		return domain.GovernanceApproval{}, domain.NewError(domain.CodeInvalidArgument, "governance approval validity is invalid")
	}
	approval := domain.GovernanceApproval{ID: command.ID, TenantID: command.TenantID, RequesterPrincipalID: command.RequesterPrincipalID,
		Action: command.Action, ResourceType: command.ResourceType, ResourceID: command.ResourceID, RequestDigest: command.RequestDigest,
		ReasonCode: command.ReasonCode, Provider: service.providerName, ProviderReference: "pending-provider-reference",
		State: domain.GovernanceApprovalPending, RequestedAt: now, ExpiresAt: command.ExpiresAt}
	if err := approval.Validate(); err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeInvalidArgument, "governance approval request is invalid", err)
	}
	reference, err := service.provider.RequestApproval(ctx, ports.ApprovalChallenge{ApprovalID: command.ID, TenantID: command.TenantID,
		RequesterPrincipalID: command.RequesterPrincipalID, Action: command.Action, ResourceType: command.ResourceType,
		ResourceID: command.ResourceID, RequestDigest: command.RequestDigest, ExpiresAt: command.ExpiresAt})
	if err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeUnavailable, "approval provider is unavailable", err).WithRetryable()
	}
	approval.ProviderReference = reference
	if err := approval.Validate(); err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeInvalidArgument, "governance approval request is invalid", err)
	}
	if err := service.store.CreateGovernanceApproval(ctx, approval); err != nil {
		return domain.GovernanceApproval{}, err
	}
	return approval, nil
}

func (service *GovernanceApprovalService) RecordDecision(ctx context.Context, assertion ports.ApprovalAssertion) (domain.GovernanceApproval, error) {
	approval, err := service.store.GovernanceApproval(ctx, assertion.ApprovalID)
	if err != nil {
		return domain.GovernanceApproval{}, err
	}
	if assertion.ProviderReference != approval.ProviderReference || assertion.RequestDigest != approval.RequestDigest {
		return domain.GovernanceApproval{}, domain.NewError(domain.CodeForbidden, "approval assertion is not bound to the request")
	}
	if err := service.provider.VerifyApproval(ctx, assertion); err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeForbidden, "approval assertion could not be verified", err)
	}
	decided, err := approval.Decide(assertion.ApproverPrincipalID, assertion.Approved, assertion.DecisionReference, assertion.DecidedAt)
	if err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeConflict, "approval decision is invalid", err)
	}
	if err := service.store.RecordGovernanceApprovalDecision(ctx, decided); err != nil {
		return domain.GovernanceApproval{}, err
	}
	return decided, nil
}

// Consume atomically binds an approved request to exactly one execution. The
// caller must supply the digest of the normalized action it is about to apply.
func (service *GovernanceApprovalService) Consume(ctx context.Context, tenantID, approvalID domain.ID, requestDigest string) (domain.GovernanceApproval, error) {
	if tenantID.IsZero() || approvalID.IsZero() {
		return domain.GovernanceApproval{}, domain.NewError(domain.CodeInvalidArgument, "tenant and approval identifiers are required")
	}
	now, err := domain.RequireUTC(service.clock.Now())
	if err != nil {
		return domain.GovernanceApproval{}, domain.WrapError(domain.CodeInternal, "approval clock is invalid", err)
	}
	approval, err := service.store.GovernanceApproval(ctx, approvalID)
	if err != nil {
		return domain.GovernanceApproval{}, err
	}
	if approval.TenantID != tenantID {
		return domain.GovernanceApproval{}, domain.NewError(domain.CodeNotFound, "governance approval not found")
	}
	return service.store.ConsumeGovernanceApproval(ctx, approvalID, requestDigest, now)
}
